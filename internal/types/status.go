package types

import "time"

// DaemonStatus is written by the running noketd and read by `noket status`
// and by `noket up` while it waits for the first registration.
type DaemonStatus struct {
	PID        int       `json:"pid"`
	NodeID     string    `json:"node_id"`
	NodeName   string    `json:"name"`
	Registered bool      `json:"registered"`
	Connected  bool      `json:"connected"`
	StartedAt  time.Time `json:"started_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	LastError  string    `json:"last_error,omitempty"`
}
