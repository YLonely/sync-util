package types

//TaskStatusRequest send by syncd to request the status of a task
type TaskStatusRequest struct {
	TaskSpecifier string   `json:"task_specifier"`
	Type          SyncType `json:"type"`
	NodeID        uint     `json:"node_id"`
}

type TaskStatus int

const (
	TaskStatusUnknown TaskStatus = iota
	TaskStatusRunning
	TaskStatusFinished
	TaskStatusFailed
)

//TaskStatusResponse represents the task status response
type TaskStatusResponse struct {
	Status TaskStatus `json:"status"`
	//running or finished by which node
	NodeID uint   `json:"node_id"`
	Msg    string `json:"msg"`
}
