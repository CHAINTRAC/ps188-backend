package model

import (
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CollCheckpoints is the MongoDB collection name for the checkpoint registry.
const CollCheckpoints = "checkpoints"

// CheckpointStatus is the operational state shown on the admin/superadmin
// dashboards. "attention" flags a checkpoint that needs review.
type CheckpointStatus string

const (
	CheckpointActive    CheckpointStatus = "active"
	CheckpointAttention CheckpointStatus = "attention"
)

func (s CheckpointStatus) Valid() bool {
	switch s {
	case CheckpointActive, CheckpointAttention:
		return true
	}
	return false
}

// Checkpoint is one border/verification post. Its Code (e.g. "CP-04") is the
// stable external identifier used in URLs and stamped onto users and screenings.
// Region is authoritative here — a verifier's region is derived from its
// checkpoint, never entered directly.
type Checkpoint struct {
	ID        bson.ObjectID    `bson:"_id,omitempty"`
	Code      string           `bson:"code"`
	Region    string           `bson:"region"`
	AdminID   string           `bson:"admin_id,omitempty"` // user id hex of the managing admin
	Status    CheckpointStatus `bson:"status"`
	CreatedAt time.Time        `bson:"created_at"`
	UpdatedAt time.Time        `bson:"updated_at"`
}

// CheckpointView is the API projection.
type CheckpointView struct {
	ID        string           `json:"id"`
	Code      string           `json:"code"`
	Region    string           `json:"region"`
	AdminID   string           `json:"admin_id"`
	Status    CheckpointStatus `json:"status"`
	CreatedAt time.Time        `json:"created_at"`
	UpdatedAt time.Time        `json:"updated_at"`
}

func (c *Checkpoint) View() CheckpointView {
	return CheckpointView{
		ID:        c.ID.Hex(),
		Code:      c.Code,
		Region:    c.Region,
		AdminID:   c.AdminID,
		Status:    c.Status,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// CreateCheckpointInput is the validated payload the checkpoint service accepts.
type CreateCheckpointInput struct {
	Code    string `json:"code" binding:"required,min=2,max=32"`
	Region  string `json:"region" binding:"required,min=1,max=80"`
	AdminID string `json:"admin_id" binding:"omitempty,len=24"`
}

// UpdateCheckpointInput carries the mutable fields. A nil pointer means "leave
// unchanged"; the checkpoint code and region are immutable.
type UpdateCheckpointInput struct {
	AdminID *string           `json:"admin_id" binding:"omitempty"`
	Status  *CheckpointStatus `json:"status" binding:"omitempty"`
}

// CheckpointFilter narrows a list query. Zero values mean "no filter".
type CheckpointFilter struct {
	Region  string
	AdminID string
}

// NormalizeCheckpointCode upper-cases and trims a checkpoint code so lookups are
// case- and whitespace-insensitive.
func NormalizeCheckpointCode(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}
