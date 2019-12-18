package syncd

import (
	"context"
	"encoding/json"
	"errors"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"syscall"
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
	registered                                                     bool
	nodeID                                                         uint
	shutdown                                                       chan struct{}
	//tasks maps a specifier to a task
	tasks map[string]*task.Task

	runningTransfer *transfer
}

// transfer represents a bunch of tasks related to a image
type transfer struct {
	id    digest.Digest
	tasks []*task.Task
	retry uint
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
	}
	var err error
	if len(c.SuperNodeIP) == 0 || len(c.SuperNodePort) == 0 {
		s.supernode, err = fakeapi.NewSuperNodeAPI()
		log.Logger.Warn("no super node ip or port provided, running in single-node mode")
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
		err = s.syncLoop(ctx)
		if err != nil {
			errorC <- err
			return
		}
	}()
	return errorC
}

func (s *Server) Stop(ctx context.Context) error {
	s.shutdown <- struct{}{}
	//wait all the tasks to exit
	for {
		running := false
		for _, t := range s.tasks {
			if status, err := t.Status(ctx); err == nil && status == task.StatusRunning {
				// just ignore the err, cause it means the task is handled by other node
				running = true
			}
		}
		if !running {
			break
		}
	}
	s.log().Info("syncd server exits")
	return nil
}

func (s *Server) registerToSuperNode(ctx context.Context) error {
	s.log().Info("start to register syncd to super node")
	rand.Seed(time.Now().Unix())
	var lastErr error
	for i := 0; i < registerRetry; i++ {
		id, err := s.nodeRegister(ctx)
		if err != nil {
			lastErr = err
			s.log().WithError(err).Warn("can't register to supernode")
			time.Sleep(registerRetryInterval + time.Duration(rand.Intn(1000))*time.Millisecond)
		} else {
			s.nodeID = id
			s.registered = true
			s.log().Info("register succeeded")
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
		s.log().Info("it seems that we should sync the remote dir")
		t, err := s.newTask(ctx, "remote-dir-build", types.DirStructureSync, nil)
		if err != nil {
			return err
		}
		if t.Remote() {
			s.log().Info("someone else get the job to sync the remote dir, wait for it")
			for {
				time.Sleep(remoteDirCheckRetryInterval)
				res, err = s.remoteDirCheck(ctx)
				if err != nil {
					return err
				}
				if !res.needSync {
					break
				}
				s.log().Debug("wait other node to sync the remote dir")
			}
			return nil
		}
		s.log().Info("get the job to build the remote dir")
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
		srcRepository      *dockertypes.RepositoryStore
		targetFlattenRepos *dockertypes.FlattenRepos
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
	s.log().Info("start synchronizing...")
	ticker := time.NewTicker(syncLoopInterval)
	for {

		if s.runningTransfer == nil {
			srcRepository = &dockertypes.RepositoryStore{}
			targetFlattenRepos = &dockertypes.FlattenRepos{}
			//we only pick one runnable image transfer to run
			if err := readJSONFile(srcRepositoryFilePath, srcRepository); err != nil {
				return err
			}
			if err := readJSONFile(s.targetRepositoryFilePath, targetFlattenRepos); err != nil {
				return err
			}
			srcFlattenRepos := srcRepository.GetAllRepos()
			diffRepos := calcRepoDiff(srcFlattenRepos, *targetFlattenRepos)
			if len(diffRepos) == 0 {
				s.log().Debug("no repos need to be synchronized")
			} else {
				repos := make([]string, 0, len(diffRepos))
				for _, v := range diffRepos {
					repos = append(repos, v.Encoded()[:10])
				}
				s.log().WithField("different-repos", repos).Debug()
			}

			// start to dispatch tasks
			for _, repo := range diffRepos {
				tasks, err := s.newImageTransferTasks(cctx, repo)
				if err != nil {
					s.log().WithField("image-id", repo.Encoded()[:10]).WithError(err).Error("failed to get tasks")
					continue
				}
				if len(tasks) == 0 {
					s.log().WithField("image-id", repo.Encoded()[:10]).Warn("other node is transferring this image")
					continue
				} else {
					needSyncLayers := make([]string, 0, len(tasks))
					for _, v := range tasks {
						needSyncLayers = append(needSyncLayers, v.Specifier()[:10])
					}
					s.log().WithFields(logrus.Fields{
						"image-id":         repo.Encoded()[:10],
						"need-sync-layers": needSyncLayers,
					}).Debug()
				}
				s.runningTransfer = &transfer{id: repo, tasks: tasks}
				for _, t := range tasks {
					if _, exists := s.tasks[t.Specifier()]; exists {
						s.log().WithFields(logrus.Fields{
							"image-id":  repo.Encoded()[:10],
							"specifier": t.Specifier()[:10],
						}).Debug("task already exists")
						//the task is the same, we don't do it again, but the repo which contains those
						//tasks may be different, so we should let it succeed, especially in single-node mode
						t.SetJob(func(ctx context.Context) error { return nil })
					} else {
						s.tasks[t.Specifier()] = t
					}
					go t.Run(cctx)
				}
				//well, find one, just break
				break
			}
		}

		// is this running transfer succeeded and should be added to repository file
		// or it failed should be tagged as failedTransfer
		if s.runningTransfer != nil {
			var (
				succeeded  = true
				terminated = true
				result     error
			)
			for _, t := range s.runningTransfer.tasks {
				status, err := t.Status(cctx)
				if err != nil {
					status = task.StatusRunning
					result = nil
					s.log().WithField("specifier", t.Specifier()[:10]).WithError(err).Warn("failed to get the status")
				} else if status == task.StatusFailed {
					result = t.Result()
				}
				if status != task.StatusFailed && status != task.StatusFinished {
					terminated = false
				}
				if status != task.StatusFinished {
					succeeded = false
				}
				if status == task.StatusFailed {
					if t.Remote() {
						s.log().WithField("specifier", t.Specifier()[:10]).Warn("remote task failed")
					} else {
						s.log().WithField("specifier", t.Specifier()[:10]).WithError(result).Error()
					}
				}
			}
			if succeeded {
				// this is not a good idea i think, cause it will influence the whole system
				// but if we don't sync before lock, the AtomicWriteFile will waste too much time, which leads to the timeout of the lock
				syscall.Sync()
				err := s.lock(cctx)
				if err != nil {
					return err
				}
				targetFlattenRepos = &dockertypes.FlattenRepos{}
				if err = readJSONFile(s.targetRepositoryFilePath, targetFlattenRepos); err != nil {
					return err
				}
				(*targetFlattenRepos)[s.runningTransfer.id] = struct{}{}
				data, err := json.Marshal(targetFlattenRepos)
				if err != nil {
					return err
				}
				ioutils.AtomicWriteFile(s.targetRepositoryFilePath, data, 0644)
				if err = s.unLock(cctx); err != nil {
					return err
				}
				s.log().WithField("succeeded-image", s.runningTransfer.id.Encoded()[:10]).Debug()
				s.runningTransfer = nil
			} else if terminated {
				for _, t := range s.runningTransfer.tasks {
					status, err := t.Status(cctx)
					if err != nil {
						panic(err)
					}
					if status == task.StatusFailed {
						delete(s.tasks, t.Specifier())
					}
				}
				s.runningTransfer = nil
			}
		}

		select {
		case <-s.shutdown:
			exit = true
		case <-ticker.C:
		}
		if exit {
			break
		}
	}
	return nil
}

func (s *Server) newImageTransferTasks(ctx context.Context, repo digest.Digest) ([]*task.Task, error) {
	algo, imageID := repo.Algorithm().String(), repo.Encoded()
	imageConfigFilePath := filepath.Join(s.metaDataDir, "imagedb", "content", algo, imageID)
	var (
		imageConfig = &image.Image{}
		configByte  []byte
		err         error
		chainIDs    []digest.Digest
		tasks       []*task.Task
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

	for _, id := range chainIDs {
		t, err := s.newLayerTransferTask(ctx, id)
		if err != nil {
			return tasks, err
		}
		tasks = append(tasks, t)
	}

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
	return s.newTask(ctx, chainID.Encoded(), types.DefaultSync, layerTransferJob)
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

func (s *Server) newTask(ctx context.Context, specifier string, t types.SyncType, job task.JobType) (*task.Task, error) {
	return task.NewTask(ctx, s.supernode, s.nodeID, specifier, t, job)
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
	timer := time.NewTimer(lockRetryInterval + time.Duration(rand.Intn(1000))*time.Millisecond)

	for {
		res, err = s.supernode.Lock(ctx, req)
		if err != nil {
			break
		}
		if res.Result == types.ResponseTypeSucceeded {
			return nil
		}
		s.log().Debugf("failed to get lock, lock is occupied by node %v", res.OccupiedBy)
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			timer.Reset(lockRetryInterval + time.Duration(rand.Intn(1000))*time.Millisecond)
		}
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
	if res.Result == types.ResponseTypeFailed {
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

func (s *Server) log() *logrus.Entry {
	if s.registered {
		return log.Logger.WithField("node-id", s.nodeID)
	}
	return log.Logger.WithField("node-id", "unknown")
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
