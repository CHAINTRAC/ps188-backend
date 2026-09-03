package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CollAuditLogs is the MongoDB collection name for the append-only audit trail.
const CollAuditLogs = "audit_logs"

// Audit action constants — one per state-changing operation.
const (
	ActionUserCreated          = "user.created"
	ActionUserPasswordChanged  = "user.password_changed"
	ActionUserPasswordReset    = "user.password_reset"
	ActionAuthLogin            = "auth.login"
	ActionScreeningSubmitted   = "screening.submitted"
	ActionScreeningDecided     = "screening.decided"
	ActionBlacklistAdded       = "blacklist.added"
	ActionBlacklistDeactivated = "blacklist.deactivated"
	ActionCheckpointCreated    = "checkpoint.created"
	ActionCheckpointUpdated    = "checkpoint.updated"
)

// AuditLog is written on every state-changing operation and never updated or
// deleted. OldData/NewData capture before/after state for investigations.
type AuditLog struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	UserID        string        `bson:"user_id"`
	Action        string        `bson:"action"`
	ReferenceType string        `bson:"reference_type"`
	ReferenceID   string        `bson:"reference_id"`
	OldData       bson.M        `bson:"old_data,omitempty"`
	NewData       bson.M        `bson:"new_data,omitempty"`
	IPAddress     string        `bson:"ip_address,omitempty"`
	CreatedAt     time.Time     `bson:"created_at"`
}
