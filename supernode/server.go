package supernode

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/YLonely/sync-util/supernode/urls"

	"github.com/YLonely/sync-util/syncd/ioutils"

	"github.com/YLonely/sync-util/log"

	"github.com/YLonely/sync-util/api/types"
)

//Config containes all the configurable properties
type Config struct {
	Port string
}

//NewServer returns a supernode instance
func NewServer(c Config) (*Server, error) {
	s := &Server{
		server: &http.Server{
			Addr: ":" + c.Port,
		},
		tasks:    map[types.SyncType]map[string]uint{},
		jsonPath: filepath.Join(homeDir, "server.json"),
	}
	s.tasks[types.DefaultSync] = map[string]uint{}
	s.tasks[types.DirStructureSync] = map[string]uint{}
	if err := s.reload(); err != nil {
		return nil, err
	}
	return s, nil
}

//Server represents the supernode server
type Server struct {
	server     *http.Server
	MaxNodeID  uint
	idMutex    sync.Mutex
	tasks      map[types.SyncType]map[string]uint
	taskMutex  sync.Mutex
	lockNodeID uint
	lockMutex  sync.Mutex
	jsonPath   string
	lockIndex  uint64
}

const (
	homeDir = "/var/lib/sync-util/supernode"
)

func (s *Server) Start() chan error {
	errorC := make(chan error, 1)
	s.initHandler()
	if err := os.MkdirAll(homeDir, 0644); err != nil {
		errorC <- err
		return errorC
	}

	go func() {
		if err := s.server.ListenAndServe(); err != nil {
			errorC <- err
		}
	}()
	log.Logger.Info("supernode successfully booted")

	return errorC
}

func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

func (s *Server) nodeRegister(rw http.ResponseWriter, r *http.Request) {
	resp := &types.NodeRegisterResponse{}
	s.idMutex.Lock()
	defer s.idMutex.Unlock()
	s.MaxNodeID++
	if err := s.save(); err != nil {
		log.Logger.WithError(err).Error()
		return
	}
	log.Logger.WithFields(logrus.Fields{
		"type":    "node register",
		"node-id": s.MaxNodeID,
	}).Debug()
	resp.NodeID = s.MaxNodeID
	if err := encodeResponse(rw, http.StatusOK, resp); err != nil {
		log.Logger.WithError(err).Error()
	}
}

func (s *Server) taskRegister(rw http.ResponseWriter, r *http.Request) {
	reader := r.Body
	request := &types.TaskRegisterRequest{}
	if err := json.NewDecoder(reader).Decode(request); err != nil {
		log.Logger.WithError(err).Error()
		return
	}
	resp := &types.TaskRegisterResponse{
		Result: types.RegisterFailed,
	}
	specifier := request.TaskSpecifier
	if len(specifier) > 10 {
		specifier = specifier[:10]
	}
	log.Logger.WithFields(logrus.Fields{
		"type":      "task register",
		"node-id":   request.NodeID,
		"specifier": specifier,
		"task-type": request.Type,
	}).Debug()
	invalid := false
	// not absolutely safe
	if request.NodeID > s.MaxNodeID {
		resp.Msg = "invalid node id"
		invalid = true
	}
	switch request.Type {
	case types.DefaultSync:
	case types.DirStructureSync:
	default:
		invalid = true
		resp.Msg = "invalid sync type"
	}
	if invalid {
		if err := encodeResponse(rw, http.StatusOK, resp); err != nil {
			log.Logger.WithError(err).Error()
		}
		return
	}
	s.taskMutex.Lock()
	defer s.taskMutex.Unlock()
	if runningBy, exists := s.tasks[request.Type][request.TaskSpecifier]; exists {
		resp.Result = types.RegisterAlreadyExist
		resp.RunningBy = runningBy
	} else {
		resp.Result = types.RegisterSucceeded
		s.tasks[request.Type][request.TaskSpecifier] = request.NodeID
	}
	if err := encodeResponse(rw, http.StatusOK, resp); err != nil {
		log.Logger.WithError(err).Error()
	}
}

func (s *Server) lock(rw http.ResponseWriter, r *http.Request) {
	reader := r.Body
	request := &types.LockRequest{}
	if err := json.NewDecoder(reader).Decode(request); err != nil {
		log.Logger.WithError(err).Error()
	}
	resp := &types.LockResponse{}
	//TODO: valid check
	log.Logger.WithFields(logrus.Fields{
		"type":    "lock",
		"node-id": request.NodeID,
		"timeout": request.TimeOut,
	}).Debug()
	s.lockMutex.Lock()
	defer s.lockMutex.Unlock()
	if s.lockNodeID == 0 {
		resp.Result = types.LockSucceeded
		s.lockNodeID = request.NodeID
		s.lockIndex++
		go func(lockIndex uint64) {
			timer := time.NewTimer(request.TimeOut)
			<-timer.C
			s.lockMutex.Lock()
			defer s.lockMutex.Unlock()
			if s.lockIndex == lockIndex {
				s.lockNodeID = 0
			}
		}(s.lockIndex)
	} else {
		resp.Result = types.LockFailed
		resp.OccupiedBy = s.lockNodeID
	}
	if err := encodeResponse(rw, http.StatusOK, resp); err != nil {
		log.Logger.WithError(err).Error()
	}
}

func (s *Server) unlock(rw http.ResponseWriter, r *http.Request) {
	reader := r.Body
	request := &types.UnLockRequest{}
	if err := json.NewDecoder(reader).Decode(request); err != nil {
		log.Logger.WithError(err).Error()
	}
	resp := &types.UnLockResponse{}
	//TODO: valid check
	log.Logger.WithFields(logrus.Fields{
		"type":    "unlock",
		"node-id": request.NodeID,
	}).Debug()
	s.lockMutex.Lock()
	defer s.lockMutex.Unlock()
	if request.NodeID == s.lockNodeID {
		s.lockNodeID = 0
		resp.Result = types.UnLockSucceeded
	} else {
		resp.Result = types.UnLockFailed
		resp.Msg = "invalid unlock node id"
	}
	if err := encodeResponse(rw, http.StatusOK, resp); err != nil {
		log.Logger.WithError(err).Error()
	}
}

func (s *Server) reload() error {
	data, err := ioutil.ReadFile(s.jsonPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	err = json.Unmarshal(data, s)
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) save() error {
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return ioutils.AtomicWriteFile(s.jsonPath, data, 0644)
}

func encodeResponse(rw http.ResponseWriter, statusCode int, data interface{}) error {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(statusCode)
	return json.NewEncoder(rw).Encode(data)
}

func (s *Server) initHandler() {
	//http.HandleFunc
	http.HandleFunc(urls.LockPath, s.lock)
	http.HandleFunc(urls.NodeRegisterPath, s.nodeRegister)
	http.HandleFunc(urls.TaskRegisterPath, s.taskRegister)
	http.HandleFunc(urls.UnLockPath, s.unlock)
}
