package types

type UnLockResponseType int

const (
	UnLockSucceeded UnLockResponseType = iota
	UnLockFailed
)

type UnLockResponse struct {
	Result UnLockResponseType `json:"result"`
	Msg    string             `json:"msg"`
}
