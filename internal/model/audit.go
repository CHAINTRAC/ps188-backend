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
// deleted. Region is the event's own region (e.g. the checkpoint created),
// not the actor's — denormalised at write time like Screening.Region.
type AuditLog struct {
	ID            bson.ObjectID `bson:"_id,omitempty"`
	UserID        string        `bson:"user_id"`
	Action        string        `bson:"action"`
	Region        string        `bson:"region,omitempty"`
	ReferenceType string        `bson:"reference_type"`
	ReferenceID   string        `bson:"reference_id"`
	OldData       bson.M        `bson:"old_data,omitempty"`
	NewData       bson.M        `bson:"new_data,omitempty"`
	IPAddress     string        `bson:"ip_address,omitempty"`
	CreatedAt     time.Time     `bson:"created_at"`
}

// AuditLogView is the client-safe projection.
type AuditLogView struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	Action        string    `json:"action"`
	Region        string    `json:"region,omitempty"`
	ReferenceType string    `json:"reference_type"`
	ReferenceID   string    `json:"reference_id"`
	OldData       bson.M    `json:"old_data,omitempty"`
	NewData       bson.M    `json:"new_data,omitempty"`
	IPAddress     string    `json:"ip_address,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func (a *AuditLog) View() AuditLogView {
	return AuditLogView{
		ID:            a.ID.Hex(),
		UserID:        a.UserID,
		Action:        a.Action,
		Region:        a.Region,
		ReferenceType: a.ReferenceType,
		ReferenceID:   a.ReferenceID,
		OldData:       a.OldData,
		NewData:       a.NewData,
		IPAddress:     a.IPAddress,
		CreatedAt:     a.CreatedAt,
	}
}

// AuditFilter narrows a list query. Zero values mean "no filter". UserID and
// Region are overwritten by middleware.ScopeAuditToActor — never trust them
// as sent by the client.
type AuditFilter struct {
	UserID string
	Action string
	Region string
	Deny   bool // set for a misconfigured admin (no region) — fail closed, not unscoped
}
