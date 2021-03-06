package types

type SyncType int

const (
	//DefaultSync task syncs the image meta data and layer data to remote dir
	DefaultSync SyncType = iota
	//DirStructureSync build the dir structure for the remote dir
	DirStructureSync
)

//TaskRegisterRequest send by syncd to register a sync tack
type TaskRegisterRequest struct {
	NodeID        uint     `json:"node_id"`
	TaskSpecifier string   `json:"task_specifier"`
	Type          SyncType `json:"type"`
}

//TaskRegisterResponse represents the register response
type TaskRegisterResponse struct {
	Result     ResponseType `json:"result"`
	TaskStatus TaskStatus   `json:"task_status"`
	NodeID     uint         `json:"node_id"`
	Msg        string       `json:"msg"`
}
