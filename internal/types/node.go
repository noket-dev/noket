package types

// Node is the persisted node identity. A node belongs to the account that first
// registered it, so the identity is kept per account: switching accounts on one
// machine yields a separate node instead of reusing an id owned by someone else.
type Node struct {
	ID     string `json:"id"`
	UserID string `json:"user_id,omitempty"`
}
