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
		Region:        created.Region,
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

// Update changes an account's status, scope (region / checkpoint), or role.
// A super admin may edit anyone; an admin may edit only a verifier in their own
// region and may never change a role. Nobody may disable their own account or
// change their own role. A role change re-derives the scope for the new role
// (checkpoint required for verifier, region for admin, both cleared for super
// admin). Writes an audit entry — user.role_changed / user.disabled /
// user.updated, most-specific wins.
func (s *UserService) Update(ctx context.Context, actor jwt.TokenData, ip, targetID string, in model.UpdateUserInput) (model.UserView, error) {
	var zero model.UserView

	target, err := s.users.FindByID(ctx, targetID)
	if err != nil {
		return zero, err
	}

	isSuper := actor.Role == string(model.RoleSuperAdmin)
	isAdmin := actor.Role == string(model.RoleAdmin)

	switch {
	case isSuper:
		// may edit anyone
	case isAdmin:
		if target.Role != model.RoleVerifier || target.Region == "" || target.Region != actor.Region {
			return zero, apperr.ERRORS.Forbidden
		}
	default:
		return zero, apperr.ERRORS.Forbidden
	}

	self := targetID == actor.UserID

	// Resolve the post-update role.
	newRole := target.Role
	roleChanged := false
	if in.Role != nil && *in.Role != target.Role {
		if !isSuper || self {
			return zero, apperr.ERRORS.CannotModifySelf
		}
		if !in.Role.Valid() {
			return zero, apperr.ERRORS.InvalidRole
		}
		newRole, roleChanged = *in.Role, true
	}

	set := bson.M{}
	if roleChanged {
		set["role"] = newRole
	}

	region, checkpointID := target.Region, target.CheckpointID
	switch newRole {
	case model.RoleVerifier:
		cp := checkpointID
		if in.CheckpointID != nil {
			cp = model.NormalizeCheckpointCode(*in.CheckpointID)
		}
		if cp == "" {
			return zero, apperr.ERRORS.MissingScopeField
		}
		if cp != checkpointID || roleChanged {
			r, err := s.checkpoints.Resolve(ctx, cp)
			if err != nil {
				return zero, err
			}
			if isAdmin && r != actor.Region {
				return zero, apperr.ERRORS.Forbidden // can't move a verifier out of your region
			}
			region, checkpointID = r, cp
			set["checkpoint_id"] = checkpointID
			set["region"] = region
		}
	case model.RoleAdmin:
		reg := region
		if in.Region != nil {
			reg = strings.TrimSpace(*in.Region)
		}
		if reg == "" {
			return zero, apperr.ERRORS.MissingScopeField
		}
		if reg != region || roleChanged {
			ok, err := s.checkpoints.RegionExists(ctx, reg)
			if err != nil {
				return zero, err
			}
			if !ok {
				return zero, apperr.ERRORS.UnknownRegion
			}
			region = reg
			set["region"] = region
			if roleChanged {
				checkpointID = ""
				set["checkpoint_id"] = ""
			}
		}
	case model.RoleSuperAdmin:
		if roleChanged {
			set["region"] = ""
			set["checkpoint_id"] = ""
		}
	}

	// Status.
	disabling := false
	statusChanged := false
	if in.Status != nil && *in.Status != target.Status {
		if !in.Status.Valid() {
			return zero, apperr.ERRORS.ValidationError
		}
		if self && *in.Status == model.UserDisabled {
			return zero, apperr.ERRORS.CannotModifySelf
		}
		set["status"] = *in.Status
		statusChanged = true
		disabling = *in.Status == model.UserDisabled
	}

	if len(set) == 0 {
		return target.View(), nil
	}

	updated, err := s.users.Update(ctx, targetID, set)
	if err != nil {
		return zero, err
	}

	action := model.ActionUserUpdated
	switch {
	case roleChanged:
		action = model.ActionUserRoleChanged
	case statusChanged && disabling:
		action = model.ActionUserDisabled
	}
	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        actor.UserID,
		Action:        action,
		Region:        updated.Region,
		ReferenceType: "user",
		ReferenceID:   targetID,
		OldData: bson.M{
			"role": target.Role, "status": target.Status,
			"region": target.Region, "checkpoint_id": target.CheckpointID,
		},
		NewData: bson.M{
			"role": updated.Role, "status": updated.Status,
			"region": updated.Region, "checkpoint_id": updated.CheckpointID,
			"target_username": updated.Username,
		},
		IPAddress: ip,
		CreatedAt: time.Now().UTC(),
	})
	return updated.View(), nil
}

func (s *UserService) List(ctx context.Context, f model.UserFilter, cursor string, limit int64) (response.Page[model.UserView], error) {
	return s.users.List(ctx, f, cursor, limit)
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
		Region:        u.Region,
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
		Region:        target.Region,
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
