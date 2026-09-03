package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/response"
)

// UserService manages user accounts (admin-only writes).
type UserService struct {
	users       repository.UserRepository
	audit       repository.AuditRepository
	checkpoints *CheckpointService
}

func NewUserService(users repository.UserRepository, audit repository.AuditRepository, checkpoints *CheckpointService) *UserService {
	return &UserService{users: users, audit: audit, checkpoints: checkpoints}
}

func (s *UserService) Create(ctx context.Context, actorID, ip string, in model.CreateUserInput) (model.UserView, error) {
	var zero model.UserView
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.CheckpointID = model.NormalizeCheckpointCode(in.CheckpointID)
	in.Region = strings.TrimSpace(in.Region)

	if !in.Role.Valid() {
		return zero, apperr.ERRORS.InvalidRole
	}

	// Resolve the account's region/checkpoint scope against the registry.
	region, checkpointID := "", ""
	switch in.Role {
	case model.RoleVerifier:
		if in.CheckpointID == "" {
			return zero, apperr.ERRORS.MissingScopeField
		}
		r, err := s.checkpoints.Resolve(ctx, in.CheckpointID)
		if err != nil {
			return zero, err
		}
		region, checkpointID = r, in.CheckpointID
	case model.RoleAdmin:
		if in.Region == "" {
			return zero, apperr.ERRORS.MissingScopeField
		}
		ok, err := s.checkpoints.RegionExists(ctx, in.Region)
		if err != nil {
			return zero, err
		}
		if !ok {
			return zero, apperr.ERRORS.UnknownRegion
		}
		region = in.Region
	case model.RoleSuperAdmin:
		// org-wide: no scope
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
		Region:       region,
		CheckpointID: checkpointID,
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
		NewData: bson.M{
			"username":      created.Username,
			"role":          created.Role,
			"region":        created.Region,
			"checkpoint_id": created.CheckpointID,
		},
		IPAddress: ip,
		CreatedAt: time.Now().UTC(),
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

// ChangePassword lets any authenticated user rotate their own password after
// re-verifying the current one.
func (s *UserService) ChangePassword(ctx context.Context, userID, ip, current, next string) error {
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
		return apperr.ERRORS.InvalidCurrentPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), BcryptCost)
	if err != nil {
		return apperr.ERRORS.UnhandledError.Wrap(err)
	}
	if err := s.users.UpdatePassword(ctx, userID, string(hash)); err != nil {
		return err
	}
	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        userID,
		Action:        model.ActionUserPasswordChanged,
		ReferenceType: "user",
		ReferenceID:   userID,
		IPAddress:     ip,
		CreatedAt:     time.Now().UTC(),
	})
	return nil
}

// ResetPassword issues a fresh temporary password for another account. An admin
// may reset a verifier in their own region; a super admin may reset anyone.
func (s *UserService) ResetPassword(ctx context.Context, actor jwt.TokenData, ip, targetID string) (string, error) {
	target, err := s.users.FindByID(ctx, targetID)
	if err != nil {
		return "", err
	}

	switch actor.Role {
	case string(model.RoleSuperAdmin):
		// may reset anyone
	case string(model.RoleAdmin):
		if target.Role != model.RoleVerifier || target.Region == "" || target.Region != actor.Region {
			return "", apperr.ERRORS.Forbidden
		}
	default:
		return "", apperr.ERRORS.Forbidden
	}

	temp, err := randomPassword()
	if err != nil {
		return "", apperr.ERRORS.UnhandledError.Wrap(err)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(temp), BcryptCost)
	if err != nil {
		return "", apperr.ERRORS.UnhandledError.Wrap(err)
	}
	if err := s.users.UpdatePassword(ctx, targetID, string(hash)); err != nil {
		return "", err
	}
	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        actor.UserID,
		Action:        model.ActionUserPasswordReset,
		ReferenceType: "user",
		ReferenceID:   targetID,
		NewData:       bson.M{"target_username": target.Username},
		IPAddress:     ip,
		CreatedAt:     time.Now().UTC(),
	})
	return temp, nil
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

// randomPassword returns a 16-character URL-safe temporary password.
func randomPassword() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
