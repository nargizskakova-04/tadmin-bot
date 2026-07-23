package domain

import "time"

// AccessStatus is the lifecycle state of an access request.
type AccessStatus string

const (
	AccessPending  AccessStatus = "pending"
	AccessApproved AccessStatus = "approved"
	AccessRejected AccessStatus = "rejected"
)

// AccessRequest records one user's request to use the bot. For a Telegram
// private chat the chat ID equals the user ID, so UserID doubles as the DM
// chat ID when notifying the requester of a decision.
type AccessRequest struct {
	UserID      int64        `json:"user_id"`
	Username    string       `json:"username"` // without @, may be empty
	FirstName   string       `json:"first_name"`
	Status      AccessStatus `json:"status"`
	RequestedAt time.Time    `json:"requested_at"`
	DecidedAt   time.Time    `json:"decided_at"`
}
