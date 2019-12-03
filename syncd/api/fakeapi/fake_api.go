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
		Result: types.RegisterSucceeded,
	}, nil
}

func (fake *fakeAPI) Lock(ctx context.Context, req *types.LockRequest) (*types.LockResponse, error) {
	return &types.LockResponse{
		Result: types.LockSucceeded,
	}, nil
}

func (fake *fakeAPI) UnLock(ctx context.Context, req *types.UnLockRequest) (*types.UnLockResponse, error) {
	return &types.UnLockResponse{
		Result: types.UnLockSucceeded,
	}, nil
}
