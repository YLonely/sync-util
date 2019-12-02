package types

type SyncType int

const (
	//ImageMetaDataSync syncs the image meta-data to remote dir
	ImageMetaDataSync SyncType = iota
	//LayerDataSync syncs the layer data to remote dir
	LayerDataSync
)

//TaskRegisterRequest send by syncd to register a sync tack
type TaskRegisterRequest struct {
	NodeID        uint     `json:"node_id"`
	TaskSpecifier string   `json:"task_specifier"`
	Type          SyncType `json:"type"`
}
