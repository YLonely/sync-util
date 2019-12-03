package types

import "time"

//LockRequest locks acquire the central lock
type LockRequest struct {
	NodeID  uint          `json:"node_id"`
	TimeOut time.Duration `json:"timeout"`
}
