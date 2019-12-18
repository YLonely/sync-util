package types

type TaskStatusReportRequest struct {
	NodeID        uint       `json:"node_id"`
	TaskSpecifier string     `json:"specifier"`
	TaskType      SyncType   `json:"task_type"`
	Status        TaskStatus `json:"status"`
}

type TaskStatusReportResponse struct {
	Result ResponseType `json:"result"`
	Msg    string       `json:msg"`
}
