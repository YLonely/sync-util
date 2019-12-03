package syncd

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"time"

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
	properlyClosed                                                 chan struct{}
}

const (
	registerRetryInterval       = time.Second * 5
	registerRetry               = 5
	lockRetryInterval           = time.Second * 5
	remoteDirCheckRetryInterval = time.Second * 5
)

//NewServer init a server instance
func NewServer(c Config) (*Server, error) {
	s := &Server{
		syncDir:        c.SyncDir,
		lockTimeout:    time.Second * 5,
		shutdown:       make(chan struct{}, 1),
		properlyClosed: make(chan struct{}, 1),
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
	<-s.properlyClosed
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
			log.Logger.Infof("register succeeded get node id: %v", s.nodeID)
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
				repository := dockertypes.RepositoryStore{}
				bytes, err := json.Marshal(repository)
				if err != nil {
					return err
				}
				ioutils.AtomicWriteFile(s.targetRepositoryFilePath, bytes, 0644)
			}
			return nil
		})
		t.Run(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) syncLoop(ctx context.Context) error {

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
		log.Logger.WithField("node-id", s.nodeID).Debug("failed to get lock, sleep and retry")
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
	s.targetRepositoryFilePath = filepath.Join(s.syncDir, "image", driver, "repositories.json")
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
