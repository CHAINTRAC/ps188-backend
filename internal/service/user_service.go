package service

import (
	"context"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/response"
)

// UserService manages user accounts (admin-only writes).
type UserService struct {
	users repository.UserRepository
	audit repository.AuditRepository
}

func NewUserService(users repository.UserRepository, audit repository.AuditRepository) *UserService {
	return &UserService{users: users, audit: audit}
}

func (s *UserService) Create(ctx context.Context, actorID, ip string, in model.CreateUserInput) (model.UserView, error) {
	var zero model.UserView
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))

	if !in.Role.Valid() {
		return zero, apperr.ERRORS.InvalidRole
	}

	if _, err := s.users.FindByUsername(ctx, in.Username); err == nil {
		return zero, apperr.ERRORS.UsernameTaken
	} else if ae := apperr.From(err); ae.Code != apperr.ERRORS.UserNotFound.Code {
		return zero, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), BcryptCost)
	if err != nil {
		return zero, apperr.ERRORS.UnhandledError.Wrap(err)
	}

	created, err := s.users.Create(ctx, &model.User{
		Username:     in.Username,
		FullName:     in.FullName,
		Email:        in.Email,
		PasswordHash: string(hash),
		Role:         in.Role,
		Status:       model.UserActive,
	})
	if err != nil {
		if ae := apperr.From(err); ae.Code == apperr.ERRORS.DuplicateResource.Code {
			return zero, apperr.ERRORS.EmailTaken
		}
		return zero, err
	}

	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        actorID,
		Action:        model.ActionUserCreated,
		ReferenceType: "user",
		ReferenceID:   created.ID.Hex(),
		NewData:       bson.M{"username": created.Username, "role": created.Role},
		IPAddress:     ip,
		CreatedAt:     time.Now().UTC(),
	})
	return created.View(), nil
}

func (s *UserService) List(ctx context.Context, cursor string, limit int64) (response.Page[model.UserView], error) {
	return s.users.List(ctx, cursor, limit)
}

func (s *UserService) Profile(ctx context.Context, userID string) (model.UserView, error) {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return model.UserView{}, err
	}
	return u.View(), nil
}

// SeedAdmin creates the bootstrap super admin when the users collection is
// empty. It is a no-op on every subsequent boot.
func (s *UserService) SeedAdmin(ctx context.Context, username, password, email string) (bool, error) {
	n, err := s.users.Count(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), BcryptCost)
	if err != nil {
		return false, apperr.ERRORS.UnhandledError.Wrap(err)
	}
	_, err = s.users.Create(ctx, &model.User{
		Username:     strings.ToLower(username),
		FullName:     "Bootstrap Super Admin",
		Email:        strings.ToLower(email),
		PasswordHash: string(hash),
		Role:         model.RoleSuperAdmin,
		Status:       model.UserActive,
	})
	if err != nil {
		if ae := apperr.From(err); ae.Code == apperr.ERRORS.DuplicateResource.Code {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
