package types

type LockResponseType int

const (
	LockSucceeded LockResponseType = iota
	LockFailed
)

type LockResponse struct {
	Result     LockResponseType `json:"result"`
	OccupiedBy uint             `json:"occupied_by"`
}
