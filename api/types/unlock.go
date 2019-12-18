package types

type UnLockRequest struct {
	NodeID uint `json:"node_id"`
}

type UnLockResponse struct {
	Result ResponseType `json:"result"`
	Msg    string       `json:"msg"`
}
