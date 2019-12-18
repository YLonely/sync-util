package task

import (
	"context"
	"fmt"

	"github.com/YLonely/sync-util/log"

	"github.com/YLonely/sync-util/syncd/api"

	"github.com/YLonely/sync-util/api/types"
)

type TaskStatus int

const (
	StatusFinished TaskStatus = iota
	StatusPending
	StatusRunning
	StatusFailed
)

type JobType func(ctx context.Context) error

func NewTask(ctx context.Context, api api.SuperNodeAPI, nodeID uint, specifier string, t types.SyncType, job JobType) (*Task, error) {
	res := &Task{
		api:       api,
		nodeID:    nodeID,
		specifier: specifier,
		taskType:  t,
		job:       job,
		status:    StatusPending,
	}
	if err := res.taskRegister(ctx); err != nil {
		return nil, err
	}
	return res, nil
}

type Task struct {
	api       api.SuperNodeAPI
	nodeID    uint
	specifier string
	taskType  types.SyncType
	result    error
	status    TaskStatus
	job       func(ctx context.Context) error
	//is this task running or finished by other node
	remote bool
}

func (t Task) Remote() bool {
	return t.remote
}

func (t *Task) SetJob(j JobType) {
	t.job = j
}

func (t *Task) Run(ctx context.Context) {

	//if t is a remote task, do nothing in run
	if !t.remote {
		var (
			reportStatus types.TaskStatus
			status       TaskStatus
		)
		t.status = StatusRunning
		t.result = t.job(ctx)
		if t.result != nil {
			status = StatusFailed
			reportStatus = types.TaskStatusFailed
		} else {
			status = StatusFinished
			reportStatus = types.TaskStatusFinished
		}
		// we should report the status to supernode
		req := &types.TaskStatusReportRequest{
			NodeID:        t.nodeID,
			TaskSpecifier: t.specifier,
			TaskType:      t.taskType,
			Status:        reportStatus,
		}
		resp, err := t.api.TaskStatusReport(ctx, req)
		t.status = status
		if err != nil {
			log.Logger.WithField("node-id", t.nodeID).WithField("specifier", t.specifier[:10]).WithError(err).Error()
			return
		}
		if resp.Result != types.ResponseTypeSucceeded {
			log.Logger.WithField("node-id", t.nodeID).WithField("specifier", t.specifier[:10]).Error(resp.Msg)
		}
	}
}

func (t *Task) Result() error {
	return t.result
}

func (t *Task) Status(ctx context.Context) (TaskStatus, error) {
	if !t.remote {
		return t.status, nil
	}
	if t.status == StatusFinished || t.status == StatusFailed {
		return t.status, nil
	}
	req := &types.TaskStatusRequest{
		TaskSpecifier: t.specifier,
		NodeID:        t.nodeID,
		Type:          t.taskType,
	}
	resp, err := t.api.TaskStatus(ctx, req)
	if err != nil {
		return StatusRunning, err
	}
	switch resp.Status {
	case types.TaskStatusFinished:
		t.status = StatusFinished
	case types.TaskStatusRunning:
		t.status = StatusRunning
	default:
		t.status = StatusFailed
	}
	return t.status, nil
}

func (t Task) Specifier() string {
	return t.specifier
}

func (t *Task) taskRegister(ctx context.Context) error {
	req := &types.TaskRegisterRequest{
		NodeID:        t.nodeID,
		TaskSpecifier: t.specifier,
		Type:          t.taskType,
	}
	resp, err := t.api.TaskRegister(ctx, req)
	if err != nil {
		return err
	}
	if resp.Result == types.ResponseTypeFailed {
		t.remote = true
		if resp.TaskStatus == types.TaskStatusRunning {
			//this task is running on other node
			t.status = StatusRunning
		} else if resp.TaskStatus == types.TaskStatusFinished {
			//this task is finished by other node
			t.status = StatusFinished
		} else {
			return fmt.Errorf("invalid task status type %v", resp.TaskStatus)
		}
	} else {
		t.remote = false
	}
	return nil
}
