package service

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
	"github.com/sih26/ps188-backend/internal/repository"
)

// BcryptCost is the work factor for every password hash in the system.
const BcryptCost = 12

// LoginResult is what a successful login returns.
type LoginResult struct {
	User    model.UserView `json:"user"`
	Access  string         `json:"access_token"`
	Refresh string         `json:"refresh_token"`
}

// AuthService handles credential verification and token issuance.
type AuthService struct {
	users repository.UserRepository
	audit repository.AuditRepository
	jwt   *jwt.Manager
}

func NewAuthService(users repository.UserRepository, audit repository.AuditRepository, jwtMgr *jwt.Manager) *AuthService {
	return &AuthService{users: users, audit: audit, jwt: jwtMgr}
}

// Login verifies credentials against a username or email and issues a token
// pair. On success it writes an auth.login audit entry.
func (s *AuthService) Login(ctx context.Context, identifier, password, ip string) (*LoginResult, error) {
	u, err := s.users.FindByIdentifier(ctx, identifier)
	if err != nil {
		// Never reveal whether the account exists.
		if ae := apperr.From(err); ae.Code == apperr.ERRORS.UserNotFound.Code {
			return nil, apperr.ERRORS.InvalidCredentials
		}
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, apperr.ERRORS.InvalidCredentials
	}
	if u.Status != model.UserActive {
		return nil, apperr.ERRORS.UserDisabled
	}

	td := tokenDataFor(u)
	access, err := s.jwt.CreateAccessToken(td)
	if err != nil {
		return nil, apperr.ERRORS.UnhandledError.Wrap(err)
	}
	refresh, err := s.jwt.CreateRefreshToken(td)
	if err != nil {
		return nil, apperr.ERRORS.UnhandledError.Wrap(err)
	}

	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        u.ID.Hex(),
		Action:        model.ActionAuthLogin,
		Region:        u.Region,
		ReferenceType: "user",
		ReferenceID:   u.ID.Hex(),
		NewData:       bson.M{"username": u.Username, "role": u.Role, "region": u.Region},
		IPAddress:     ip,
		CreatedAt:     time.Now().UTC(),
	})

	return &LoginResult{User: u.View(), Access: access, Refresh: refresh}, nil
}

// Refresh mints a new access token from a valid refresh token, re-checking that
// the account still exists and is active. Region/checkpoint claims are re-issued
// from the current user record.
func (s *AuthService) Refresh(ctx context.Context, refreshToken string) (string, error) {
	td, aerr := s.jwt.DecodeRefreshToken(refreshToken)
	if aerr != nil {
		return "", aerr
	}
	u, err := s.users.FindByID(ctx, td.UserID)
	if err != nil {
		return "", apperr.ERRORS.InvalidRefreshToken
	}
	if u.Status != model.UserActive {
		return "", apperr.ERRORS.UserDisabled
	}
	access, err := s.jwt.CreateAccessToken(tokenDataFor(u))
	if err != nil {
		return "", apperr.ERRORS.UnhandledError.Wrap(err)
	}
	return access, nil
}

func tokenDataFor(u *model.User) jwt.TokenData {
	return jwt.TokenData{
		UserID:       u.ID.Hex(),
		Username:     u.Username,
		Role:         string(u.Role),
		Region:       u.Region,
		CheckpointID: u.CheckpointID,
	}
}
