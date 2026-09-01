package model

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// CollUsers is the MongoDB collection name for users.
const CollUsers = "users"

// Role is a user's access level. Supervisors work the checkpoint — they submit
// screenings and record decisions; admins manage user accounts.
type Role string

const (
	RoleSupervisor Role = "supervisor"
	RoleAdmin      Role = "admin"
)

func (r Role) Valid() bool {
	switch r {
	case RoleSupervisor, RoleAdmin:
		return true
	}
	return false
}

// UserStatus controls whether the account may authenticate.
type UserStatus string

const (
	UserActive   UserStatus = "active"
	UserDisabled UserStatus = "disabled"
)

// User is the stored document. PasswordHash never leaves the repository layer.
type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty"`
	Username     string        `bson:"username"`
	FullName     string        `bson:"full_name"`
	Email        string        `bson:"email"`
	PasswordHash string        `bson:"password_hash"`
	Role         Role          `bson:"role"`
	Status       UserStatus    `bson:"status"`
	CreatedAt    time.Time     `bson:"created_at"`
	UpdatedAt    time.Time     `bson:"updated_at"`
}

// UserView is the client-safe projection — no password hash.
type UserView struct {
	ID        string     `json:"id"`
	Username  string     `json:"username"`
	FullName  string     `json:"full_name"`
	Email     string     `json:"email"`
	Role      Role       `json:"role"`
	Status    UserStatus `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
}

func (u *User) View() UserView {
	return UserView{
		ID:        u.ID.Hex(),
		Username:  u.Username,
		FullName:  u.FullName,
		Email:     u.Email,
		Role:      u.Role,
		Status:    u.Status,
		CreatedAt: u.CreatedAt,
	}
}

// CreateUserInput is the validated payload the user service accepts.
type CreateUserInput struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	FullName string `json:"full_name" binding:"required,min=1,max=120"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Role     Role   `json:"role" binding:"required"`
}
