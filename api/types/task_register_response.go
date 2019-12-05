package types

type RegisterResponseType int

const (
	RegisterSucceeded RegisterResponseType = iota
	RegisterAlreadyExist
	RegisterFailed
)

//TaskRegisterResponse represents the register response
type TaskRegisterResponse struct {
	Result    RegisterResponseType `json:"result"`
	RunningBy uint                 `json:"running_by"`
	Msg       string               `json:"msg"`
}
