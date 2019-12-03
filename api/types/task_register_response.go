package types

type RegisterResponseType int

const (
	RegisterSucceeded RegisterResponseType = iota
	RegisterFailed
)

//TaskRegisterResponse represents the register response
type TaskRegisterResponse struct {
	Result RegisterResponseType `json:"result"`
}
