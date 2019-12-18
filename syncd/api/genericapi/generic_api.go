package genericapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/YLonely/sync-util/api/types"
	"github.com/YLonely/sync-util/supernode/urls"
	"github.com/YLonely/sync-util/syncd/api"
)

func NewSuperNodeAPI(ip, port string) (api.SuperNodeAPI, error) {
	return &genericAPI{
		nodeIP:   ip,
		nodePort: port,
		client:   &http.Client{},
		timeout:  time.Second * 3,
	}, nil
}

type genericAPI struct {
	nodeIP, nodePort string
	client           *http.Client
	timeout          time.Duration
}

var _ api.SuperNodeAPI = &genericAPI{}

const (
	contentTypeKey   = "Content-Type"
	contentTypeValue = "application/json"
	formatTemplate   = "http://%s:%s%s"
)

func (g *genericAPI) NodeRegister(ctx context.Context) (*types.NodeRegisterResponse, error) {
	url := fmt.Sprintf(formatTemplate, g.nodeIP, g.nodePort, urls.NodeRegisterPath)
	res := &types.NodeRegisterResponse{}
	err := g.do(ctx, url, "", res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (g *genericAPI) TaskRegister(ctx context.Context, req *types.TaskRegisterRequest) (*types.TaskRegisterResponse, error) {
	url := fmt.Sprintf(formatTemplate, g.nodeIP, g.nodePort, urls.TaskRegisterPath)
	res := &types.TaskRegisterResponse{}
	err := g.do(ctx, url, req, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (g *genericAPI) TaskStatus(ctx context.Context, req *types.TaskStatusRequest) (*types.TaskStatusResponse, error) {
	url := fmt.Sprintf(formatTemplate, g.nodeIP, g.nodePort, urls.TaskStatusPath)
	res := &types.TaskStatusResponse{}
	err := g.do(ctx, url, req, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (g *genericAPI) TaskStatusReport(ctx context.Context, req *types.TaskStatusReportRequest) (*types.TaskStatusReportResponse, error) {
	url := fmt.Sprintf(formatTemplate, g.nodeIP, g.nodePort, urls.TaskStatusReportPath)
	res := &types.TaskStatusReportResponse{}
	if err := g.do(ctx, url, req, res); err != nil {
		return nil, err
	}
	return res, nil
}

func (g *genericAPI) Lock(ctx context.Context, req *types.LockRequest) (*types.LockResponse, error) {
	url := fmt.Sprintf(formatTemplate, g.nodeIP, g.nodePort, urls.LockPath)
	res := &types.LockResponse{}
	err := g.do(ctx, url, req, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (g *genericAPI) UnLock(ctx context.Context, req *types.UnLockRequest) (*types.UnLockResponse, error) {
	url := fmt.Sprintf(formatTemplate, g.nodeIP, g.nodePort, urls.UnLockPath)
	res := &types.UnLockResponse{}
	err := g.do(ctx, url, req, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (g *genericAPI) do(ctx context.Context, url string, req interface{}, resp interface{}) error {
	var (
		code int
		body []byte
		err  error
	)
	if code, body, err = g.postJSON(ctx, url, req, g.timeout); err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("%v:%v", code, body)
	}
	if err = json.Unmarshal(body, resp); err != nil {
		return err
	}
	return nil
}

func (g *genericAPI) postJSON(ctx context.Context, url string, body interface{}, timeout time.Duration) (int, []byte, error) {
	var (
		jsonByte []byte
		err      error
	)
	if body != nil {
		jsonByte, err = json.Marshal(body)
		if err != nil {
			return http.StatusBadRequest, nil, err
		}
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(jsonByte))
	if err != nil {
		return http.StatusBadRequest, nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req = req.WithContext(cctx)
	req.Header.Set(contentTypeKey, contentTypeValue)
	resp, err := g.client.Do(req)

	// log.Logger.WithField("url", url).WithField("body", body).Debug("post json")

	if err != nil {
		return http.StatusBadRequest, nil, err
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}
