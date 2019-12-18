package fakeapi

import (
	"context"

	"github.com/YLonely/sync-util/api/types"
	"github.com/YLonely/sync-util/syncd/api"
)

func NewSuperNodeAPI() (api.SuperNodeAPI, error) {
	return &fakeAPI{}, nil
}

type fakeAPI struct {
}

var _ api.SuperNodeAPI = &fakeAPI{}

func (fake *fakeAPI) NodeRegister(ctx context.Context) (*types.NodeRegisterResponse, error) {
	return &types.NodeRegisterResponse{}, nil
}

func (fake *fakeAPI) TaskRegister(ctx context.Context, req *types.TaskRegisterRequest) (*types.TaskRegisterResponse, error) {
	return &types.TaskRegisterResponse{
		Result: types.ResponseTypeSucceeded,
	}, nil
}

func (fake *fakeAPI) Lock(ctx context.Context, req *types.LockRequest) (*types.LockResponse, error) {
	return &types.LockResponse{
		Result: types.ResponseTypeSucceeded,
	}, nil
}

func (fake *fakeAPI) UnLock(ctx context.Context, req *types.UnLockRequest) (*types.UnLockResponse, error) {
	return &types.UnLockResponse{
		Result: types.ResponseTypeSucceeded,
	}, nil
}

func (fake *fakeAPI) TaskStatus(ctx context.Context, req *types.TaskStatusRequest) (*types.TaskStatusResponse, error) {
	return &types.TaskStatusResponse{}, nil
}

func (fake *fakeAPI) TaskStatusReport(ctx context.Context, req *types.TaskStatusReportRequest) (*types.TaskStatusReportResponse, error) {
	return &types.TaskStatusReportResponse{
		Result: types.ResponseTypeSucceeded,
	}, nil
}
