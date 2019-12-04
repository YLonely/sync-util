package syncd

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/docker/docker/image"

	"github.com/opencontainers/go-digest"

	"github.com/YLonely/sync-util/syncd/task"

	"github.com/YLonely/sync-util/syncd/dockertypes"
	"github.com/YLonely/sync-util/syncd/ioutils"

	"github.com/YLonely/sync-util/api/types"

	"github.com/YLonely/sync-util/log"

	"github.com/YLonely/sync-util/syncd/api/genericapi"

	"github.com/YLonely/sync-util/syncd/api/fakeapi"

	"github.com/YLonely/sync-util/syncd/api"

	"github.com/docker/docker/client"
)

//Config contains all configurable properties
type Config struct {
	SuperNodeIP, SuperNodePort string
	SyncDir                    string
}

//Server represents a syncd server
type Server struct {
	metaDataDir, layerDataDir                                      string
	targetLayerDBDir, targetLayerDataDir, targetRepositoryFilePath string
	syncDir                                                        string
	supernode                                                      api.SuperNodeAPI
	lockTimeout                                                    time.Duration
	nodeID                                                         uint
	shutdown                                                       chan struct{}
	//tasks maps a specifier to a task
	tasks map[string]*task.Task
	//transfer represents a bunch of tasks related to an image
	//transfers records a bunch of transfers
	transfers map[digest.Digest][]*task.Task
}

const (
	registerRetryInterval       = time.Second * 5
	registerRetry               = 5
	lockRetryInterval           = time.Second * 5
	remoteDirCheckRetryInterval = time.Second * 5
	syncLoopInterval            = time.Second * 30

	repositoryFileName = "repositories.json"
)

//NewServer init a server instance
func NewServer(c Config) (*Server, error) {
	s := &Server{
		syncDir:     c.SyncDir,
		lockTimeout: time.Second * 5,
		shutdown:    make(chan struct{}, 1),
		tasks:       map[string]*task.Task{},
		transfers:   map[digest.Digest][]*task.Task{},
	}
	var err error
	if len(c.SuperNodeIP) == 0 || len(c.SuperNodePort) == 0 {
		s.supernode, err = fakeapi.NewSuperNodeAPI()
		log.Logger.Warn("no super node ip or port provided, use fake api instead")
	} else {
		s.supernode, err = genericapi.NewSuperNodeAPI(c.SuperNodeIP, c.SuperNodePort)
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) Start(ctx context.Context) chan error {
	errorC := make(chan error, 1)
	go func() {
		defer close(errorC)
		driver, dockerRoot, err := getDockerInfo(ctx)
		if err != nil {
			errorC <- err
			return
		}
		s.initDir(driver, dockerRoot)
		// register this node to supernode
		err = s.registerToSuperNode(ctx)
		if err != nil {
			errorC <- err
			return
		}
		// check and build the remote dir
		err = s.remoteDirInit(ctx)
		if err != nil {
			errorC <- err
			return
		}
		// start the sync loop
	}()
	return errorC
}

func (s *Server) Stop(ctx context.Context) error {
	s.shutdown <- struct{}{}
	return nil
}

func (s *Server) registerToSuperNode(ctx context.Context) error {
	log.Logger.Info("start to register syncd to super node")
	rand.Seed(time.Now().Unix())
	var lastErr error
	for i := 0; i < registerRetry; i++ {
		id, err := s.nodeRegister(ctx)
		if err != nil {
			lastErr = err
			log.Logger.WithError(err).Warn("can not register now, sleep and retry")
			time.Sleep(registerRetryInterval + time.Duration(rand.Intn(1000))*time.Millisecond)
		} else {
			s.nodeID = id
			log.Logger.Infof("register succeeded, get node id: %v", s.nodeID)
			return nil
		}
	}
	return lastErr
}

func (s *Server) remoteDirInit(ctx context.Context) error {
	res, err := s.remoteDirCheck(ctx)
	if err != nil {
		return err
	}
	if res.needSync {
		log.Logger.WithField("node-id", s.nodeID).Info("it seems that we should sync the remote dir")
		t, err := s.newRegisteredTask(ctx, "", types.DirStructureSync, nil)
		if err != nil {
			return err
		}
		if t == nil {
			log.Logger.WithField("node-id", s.nodeID).Info("someone else get the job to sync the remote dir, wait for it")
			for {
				time.Sleep(remoteDirCheckRetryInterval)
				res, err = s.remoteDirCheck(ctx)
				if err != nil {
					return err
				}
				if !res.needSync {
					break
				}
				log.Logger.WithField("node-id", s.nodeID).Debug("again, wait other node to sync the remote dir")
			}
			return nil
		}
		log.Logger.WithField("node-id", s.nodeID).Info("get the job to sync the remote dir")
		t.SetJob(func(ctx context.Context) error {
			if !res.layerDBDirExist {
				err := os.MkdirAll(s.targetLayerDBDir, 0644)
				if err != nil {
					return err
				}
			}
			if !res.layerDataDirExist {
				err := os.MkdirAll(s.targetLayerDataDir, 0644)
				if err != nil {
					return err
				}
			}
			if !res.repositoryFileExist {
				repository := &dockertypes.FlattenRepos{}
				bytes, err := json.Marshal(repository)
				if err != nil {
					return err
				}
				ioutils.AtomicWriteFile(s.targetRepositoryFilePath, bytes, 0644)
			}
			return nil
		})
		t.Run(ctx)
		err = t.Result()
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) syncLoop(ctx context.Context) error {
	var (
		srcRepository      = &dockertypes.RepositoryStore{}
		targetFlattenRepos = &dockertypes.FlattenRepos{}
		exit               bool
	)

	srcRepositoryFilePath := filepath.Join(s.metaDataDir, repositoryFileName)
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()

	readJSONFile := func(filePath string, x interface{}) error {
		var (
			content []byte
			err     error
		)
		if content, err = ioutil.ReadFile(filePath); err != nil {
			return err
		}
		if err = json.Unmarshal(content, x); err != nil {
			return err
		}
		return nil
	}
	log.Logger.WithField("node-id", s.nodeID).Info("start sync-loop....")

	for {
		select {
		case <-s.shutdown:
			exit = true
		default:
		}
		if exit {
			break
		}

		if err := readJSONFile(srcRepositoryFilePath, srcRepository); err != nil {
			return err
		}
		if err := readJSONFile(s.targetRepositoryFilePath, targetFlattenRepos); err != nil {
			return err
		}
		srcFlattenRepos := srcRepository.GetAllRepos()
		log.Logger.WithFields(logrus.Fields{
			"node-id": s.nodeID,
		}).Debugf("get all repos from src file %v and target file %v", srcFlattenRepos, *targetFlattenRepos)

		diffRepos := calcRepoDiff(srcFlattenRepos, *targetFlattenRepos)

		// start to dispatch tasks
		for _, repo := range diffRepos {
			log.Logger.WithField("node-id", s.nodeID).WithField("image-id", repo).Debug("ready to transfer")
			if _, exists := s.transfers[repo]; exists {
				log.Logger.WithField("node-id", s.nodeID).WithField("image-id", repo).Debug("transfer already exists, skip this one")
				continue
			}
			tasks, err := s.newImageTransferTasks(ctx, repo)
			if err != nil {
				return err
			}
			if len(tasks) == 0 {
				log.Logger.WithField("node-id", s.nodeID).WithField("image-id", repo.String()).Warn("image has zero task to run")
			}
			s.transfers[repo] = tasks
			for _, t := range tasks {
				if _, exists := s.tasks[t.Specifier()]; exists {
					log.Logger.WithFields(logrus.Fields{
						"node-id":   s.nodeID,
						"image-id":  repo.String(),
						"specifier": t.Specifier(),
					}).Debugf("task already exists")
					continue
				}
				s.tasks[t.Specifier()] = t
				go t.Run(cctx)
			}
		}

		//here we find some finished and succeeded image transfers to add to the repositories.json
		var finishedReposToAdd []digest.Digest
		for imageID, tasks := range s.transfers {
			succeeded := true
			for _, t := range tasks {
				if t.Status() != task.StatusFinished || t.Result() != nil {
					succeeded = false
					break
				}
			}
			if succeeded {
				finishedReposToAdd = append(finishedReposToAdd, imageID)
			}
		}

		//lock and write the file
		if len(finishedReposToAdd) != 0 {
			err := s.lock(ctx)
			if err != nil {
				return err
			}
			if err = readJSONFile(s.targetRepositoryFilePath, targetFlattenRepos); err != nil {
				return err
			}
			for _, v := range finishedReposToAdd {
				(*targetFlattenRepos)[v] = struct{}{}
			}
			data, err := json.Marshal(targetFlattenRepos)
			if err != nil {
				return err
			}
			ioutils.AtomicWriteFile(s.targetRepositoryFilePath, data, 0644)
			if err = s.unLock(ctx); err != nil {
				return err
			}
		}

		//do some clean ups, erase those failed transfers and tasks
		for imageID, tasks := range s.transfers {
			failed := false
			for _, t := range tasks {
				if t.Status() == task.StatusFinished && t.Result() != nil {
					log.Logger.WithFields(logrus.Fields{
						"node-id":   s.nodeID,
						"image-id":  imageID,
						"specifier": t.Specifier(),
					}).WithError(t.Result()).Error("task failed with error")
					failed = true
					delete(s.tasks, t.Specifier())
				}
			}
			if failed {
				delete(s.transfers, imageID)
			}
		}
	}
	return nil
}

func (s *Server) newImageTransferTasks(ctx context.Context, repo digest.Digest) ([]*task.Task, error) {
	algo, imageID := repo.Algorithm().String(), repo.Encoded()
	imageConfigFilePath := filepath.Join(s.metaDataDir, "imagedb", "content", algo, imageID)
	var (
		imageConfig          *image.Image
		configByte           []byte
		err                  error
		chainIDs             []digest.Digest
		tasks                []*task.Task
		needToSyncSpecifiers []string
	)
	//read image config from src dir
	if configByte, err = ioutil.ReadFile(imageConfigFilePath); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(configByte, imageConfig); err != nil {
		return nil, err
	}
	diffIDs := imageConfig.RootFS.DiffIDs
	//generate chainID for every layer
	rootFS := *image.NewRootFS()
	for _, v := range diffIDs {
		rootFS.Append(v)
		chainIDs = append(chainIDs, digest.Digest(rootFS.ChainID()))
	}
	log.Logger.WithFields(logrus.Fields{
		"node-id":  s.nodeID,
		"image-id": repo.String(),
	}).Debugf("image have tasks %v", chainIDs)

	for _, id := range chainIDs {
		t, err := s.newLayerTransferTask(ctx, id)
		if err != nil {
			return tasks, err
		} else if t != nil {
			tasks = append(tasks, t)
			needToSyncSpecifiers = append(needToSyncSpecifiers, t.Specifier())
		}
	}
	log.Logger.WithFields(logrus.Fields{
		"node-id":  s.nodeID,
		"image-id": repo.String(),
	}).Debugf("really need to sync %v", needToSyncSpecifiers)

	return tasks, nil
}

func (s *Server) newLayerTransferTask(ctx context.Context, chainID digest.Digest) (*task.Task, error) {
	algo := chainID.Algorithm().String()
	encoded := chainID.Encoded()
	srcLayerMetaDataPath := filepath.Join(s.metaDataDir, "layerdb", algo, encoded)
	targetLayerMetaDataPath := filepath.Join(s.targetLayerDBDir, algo, encoded)
	var (
		layerCacheID []byte
		err          error
	)
	if layerCacheID, err = ioutil.ReadFile(filepath.Join(srcLayerMetaDataPath, "cache-id")); err != nil {
		return nil, err
	}
	srcLayerDataPath := filepath.Join(s.layerDataDir, string(layerCacheID))
	targetLayerDataPath := filepath.Join(s.targetLayerDataDir, string(layerCacheID))
	layerTransferJob := func(ctx context.Context) error {
		if err := ioutils.AtomicDirCopy(ctx, srcLayerDataPath, targetLayerDataPath); err != nil {
			return err
		}
		if err := ioutils.AtomicDirCopy(ctx, srcLayerMetaDataPath, targetLayerMetaDataPath); err != nil {
			return err
		}
		return nil
	}
	return s.newRegisteredTask(ctx, chainID.String(), types.DefaultSync, layerTransferJob)
}

type checkResult struct {
	needSync            bool
	layerDBDirExist     bool
	layerDataDirExist   bool
	repositoryFileExist bool
}

//remoteDirCheck checks if the remote dir structure meets the requirement
func (s *Server) remoteDirCheck(ctx context.Context) (checkResult, error) {
	res := checkResult{
		needSync:            false,
		layerDataDirExist:   true,
		layerDBDirExist:     true,
		repositoryFileExist: true,
	}
	if _, err := os.Stat(s.targetLayerDBDir); err != nil {
		if os.IsNotExist(err) {
			res.needSync = true
			res.layerDBDirExist = false
		} else {
			return res, err
		}
	}
	if _, err := os.Stat(s.targetLayerDataDir); err != nil {
		if os.IsNotExist(err) {
			res.needSync = true
			res.layerDataDirExist = false
		} else {
			return res, err
		}
	}
	if _, err := os.Stat(s.targetRepositoryFilePath); err != nil {
		if os.IsNotExist(err) {
			res.needSync = true
			res.repositoryFileExist = false
		} else {
			return res, err
		}
	}
	return res, nil
}

func (s *Server) nodeRegister(ctx context.Context) (uint, error) {
	res, err := s.supernode.NodeRegister(ctx)
	if err != nil {
		return 0, err
	}
	return res.NodeID, nil
}

func (s *Server) newRegisteredTask(ctx context.Context, specifier string, t types.SyncType, job task.JobType) (*task.Task, error) {
	req := &types.TaskRegisterRequest{
		NodeID:        s.nodeID,
		TaskSpecifier: specifier,
		Type:          t,
	}
	resp, err := s.supernode.TaskRegister(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.Result == types.RegisterFailed {
		log.Logger.WithField("node-id", s.nodeID).Debugf("task %v is running or finished by node %v", specifier, resp.RunningBy)
		return nil, nil
	}
	res := task.NewTask(specifier, t, job)
	return res, nil
}

//lock is a blocking call
func (s *Server) lock(ctx context.Context) error {
	req := &types.LockRequest{
		NodeID:  s.nodeID,
		TimeOut: s.lockTimeout,
	}
	var (
		res *types.LockResponse
		err error
	)
	rand.Seed(time.Now().Unix())

	for {
		res, err = s.supernode.Lock(ctx, req)
		if err != nil {
			break
		}
		if res.Result == types.LockSucceeded {
			return nil
		}
		log.Logger.WithField("node-id", s.nodeID).Debugf("failed to get lock, lock is occupied by node %v", res.OccupiedBy)
		time.Sleep(lockRetryInterval + time.Duration(rand.Intn(1000))*time.Millisecond)
	}
	return err
}

func (s *Server) unLock(ctx context.Context) error {
	req := &types.UnLockRequest{
		NodeID: s.nodeID,
	}
	res, err := s.supernode.UnLock(ctx, req)
	if err != nil {
		return err
	}
	if res.Result == types.UnLockFailed {
		return errors.New(res.Msg)
	}
	return nil
}

func (s *Server) initDir(driver, dockerRoot string) {
	s.metaDataDir = filepath.Join(dockerRoot, "image", driver)
	s.layerDataDir = filepath.Join(dockerRoot, driver)
	s.targetLayerDBDir = filepath.Join(s.syncDir, "image", driver, "layerdb")
	s.targetLayerDataDir = filepath.Join(s.syncDir, driver)
	s.targetRepositoryFilePath = filepath.Join(s.syncDir, "image", driver, repositoryFileName)
}

func getDockerInfo(ctx context.Context) (string, string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return "", "", err
	}
	info, err := cli.Info(ctx)
	if err != nil {
		return "", "", err
	}
	return info.Driver, info.DockerRootDir, nil
}

//calcRepoDiff finds those repos exists in src but dont exist in target
func calcRepoDiff(src, target dockertypes.FlattenRepos) []digest.Digest {
	diff := []digest.Digest{}

	for d := range src {
		if _, exists := target[d]; !exists {
			diff = append(diff, d)
		}
	}

	return diff
}
