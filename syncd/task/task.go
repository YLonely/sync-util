package task

import (
	"context"

	"github.com/YLonely/sync-util/api/types"
)

type TaskStatus int

const (
	StatusFinished TaskStatus = iota
	StatusPending
	StatusRunning
)

type JobType func(ctx context.Context) error

func NewTask(specifier string, t types.SyncType, job JobType) *Task {
	res := &Task{
		specifier: specifier,
		taskType:  t,
		job:       job,
		status:    StatusPending,
	}
	return res
}

type Task struct {
	specifier string
	taskType  types.SyncType
	result    error
	status    TaskStatus
	job       func(ctx context.Context) error
}

func (t *Task) SetJob(j JobType) {
	t.job = j
}

func (t *Task) Run(ctx context.Context) {
	t.status = StatusRunning
	t.result = t.job(ctx)
	t.status = StatusFinished
}

func (t *Task) Result() error {
	return t.result
}

func (t Task) Status() TaskStatus {
	return t.status
}

func (t Task) Specifier() string {
	return t.specifier
}
