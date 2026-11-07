package types

import "time"

// Credentials is the local login state. Token is the same JWT the web app
// uses; it is only ever read from this file.
type Credentials struct {
	Token   string    `json:"token"`
	UserID  string    `json:"user_id,omitempty"`
	SavedAt time.Time `json:"saved_at"`
}
