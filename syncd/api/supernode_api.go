package api

import (
	"context"

	"github.com/YLonely/sync-util/api/types"
)

type SuperNodeAPI interface {
	NodeRegister(ctx context.Context) (*types.NodeRegisterResponse, error)
	TaskRegister(ctx context.Context, req *types.TaskRegisterRequest) (*types.TaskRegisterResponse, error)
	Lock(ctx context.Context, req *types.LockRequest) (*types.LockResponse, error)
	UnLock(ctx context.Context, req *types.UnLockRequest) (*types.UnLockResponse, error)
	TaskStatus(ctx context.Context, req *types.TaskStatusRequest) (*types.TaskStatusResponse, error)
	TaskStatusReport(ctx context.Context, req *types.TaskStatusReportRequest) (*types.TaskStatusReportResponse, error)
}
