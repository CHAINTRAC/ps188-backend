package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CollUsers is the MongoDB collection name for users.
const CollUsers = "users"

// Role is a user's access level. Verifiers work the checkpoint — they submit
// screenings and record decisions. Admins manage verifier accounts and the
// blacklist for their region. Super admins manage admins and everything an
// admin can, org-wide. Role names match the operator-facing UI.
type Role string

const (
	RoleVerifier   Role = "verifier"
	RoleAdmin      Role = "admin"
	RoleSuperAdmin Role = "superadmin"
)

func (r Role) Valid() bool {
	switch r {
	case RoleVerifier, RoleAdmin, RoleSuperAdmin:
		return true
	}
	return false
}

// AtLeastAdmin reports whether the role carries admin privileges (admin or
// super admin). Used to gate account- and blacklist-management routes.
func (r Role) AtLeastAdmin() bool {
	return r == RoleAdmin || r == RoleSuperAdmin
}

// UserStatus controls whether the account may authenticate.
type UserStatus string

const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)

func (s UserStatus) Valid() bool {
	switch s {
	case UserActive, UserDisabled:
		return true
	}
	return false
}

// User is the stored document. PasswordHash never leaves the repository layer.
//
// Region and CheckpointID scope what a user can see and do. A verifier is bound
// to one checkpoint (and inherits that checkpoint's region); an admin is bound
// to a region; a super admin has both empty, meaning org-wide.
type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty"`
	Username     string        `bson:"username"`
	FullName     string        `bson:"full_name"`
	Email        string        `bson:"email"`
	PasswordHash string        `bson:"password_hash"`
	Role         Role          `bson:"role"`
	Status       UserStatus    `bson:"status"`
	Region       string        `bson:"region,omitempty"`
	CheckpointID string        `bson:"checkpoint_id,omitempty"`
	CreatedAt    time.Time     `bson:"created_at"`
	UpdatedAt    time.Time     `bson:"updated_at"`
}

// UserView is the client-safe projection — no password hash.
type UserView struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	FullName     string     `json:"full_name"`
	Email        string     `json:"email"`
	Role         Role       `json:"role"`
	Status       UserStatus `json:"status"`
	Region       string     `json:"region"`
	CheckpointID string     `json:"checkpoint_id"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (u *User) View() UserView {
	return UserView{
		ID:           u.ID.Hex(),
		Username:     u.Username,
		FullName:     u.FullName,
		Email:        u.Email,
		Role:         u.Role,
		Status:       u.Status,
		Region:       u.Region,
		CheckpointID: u.CheckpointID,
		CreatedAt:    u.CreatedAt,
	}
}

// CreateUserInput is the validated payload the user service accepts. Region is
// required when Role is admin; CheckpointID is required when Role is verifier
// (its region is resolved from the checkpoint). Both are ignored for a super
// admin. Cross-field rules are enforced in the service, not by binding tags.
type CreateUserInput struct {
	Username     string `json:"username" binding:"required,min=3,max=50"`
	FullName     string `json:"full_name" binding:"required,min=1,max=120"`
	Email        string `json:"email" binding:"required,email"`
	Password     string `json:"password" binding:"required,min=8,max=128"`
	Role         Role   `json:"role" binding:"required"`
	Region       string `json:"region" binding:"omitempty,max=80"`
	CheckpointID string `json:"checkpoint_id" binding:"omitempty,max=32"`
}

// UserFilter narrows a user list query. Region is set in the handler from the
// caller's scope, not the client; Role and Status come from query params.
type UserFilter struct {
	Region string
	Role   Role
	Status UserStatus
	Deny   bool // set for a misconfigured admin (no region) — fail closed, not unscoped
}

// UpdateUserInput carries the mutable fields of an account. A nil pointer means
// "leave unchanged". Role changes are super-admin only (enforced in the service);
// changing Role also requires the matching scope field for the new role
// (CheckpointID for verifier, Region for admin).
type UpdateUserInput struct {
	Status       *UserStatus `json:"status" binding:"omitempty"`
	Role         *Role       `json:"role" binding:"omitempty"`
	Region       *string     `json:"region" binding:"omitempty,max=80"`
	CheckpointID *string     `json:"checkpoint_id" binding:"omitempty,max=32"`
}
