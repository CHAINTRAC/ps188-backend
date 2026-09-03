// Package jwt wraps token creation and verification. TokenData is the payload
// embedded in the access token and exposed as the authenticated principal on
// every protected request (see middleware.Authenticate).
package jwt

import (
	"errors"
	"time"

	jwtv5 "github.com/golang-jwt/jwt/v5"
	"github.com/sih26/ps188-backend/internal/apperr"
)

// TokenData is what an access/refresh token carries and what request handlers
// read back as the current principal.
type TokenData struct {
	UserID       string `json:"uid"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	Region       string `json:"region,omitempty"`
	CheckpointID string `json:"cp,omitempty"`
}

// Manager issues and verifies both token kinds. Construct once, inject everywhere.
type Manager struct {
	accessSecret  []byte
	refreshSecret []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
}

func NewManager(accessSecret, refreshSecret string, accessTTL, refreshTTL time.Duration) *Manager {
	return &Manager{
		accessSecret:  []byte(accessSecret),
		refreshSecret: []byte(refreshSecret),
		accessTTL:     accessTTL,
		refreshTTL:    refreshTTL,
	}
}

type claims struct {
	TokenData
	Kind string `json:"knd"` // "access" | "refresh"
	jwtv5.RegisteredClaims
}

func (m *Manager) sign(td TokenData, kind string, ttl time.Duration, secret []byte) (string, error) {
	now := time.Now()
	c := claims{
		TokenData: td,
		Kind:      kind,
		RegisteredClaims: jwtv5.RegisteredClaims{
			Subject:   td.UserID,
			IssuedAt:  jwtv5.NewNumericDate(now),
			ExpiresAt: jwtv5.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwtv5.NewWithClaims(jwtv5.SigningMethodHS256, c).SignedString(secret)
}

func (m *Manager) CreateAccessToken(td TokenData) (string, error) {
	return m.sign(td, "access", m.accessTTL, m.accessSecret)
}

func (m *Manager) CreateRefreshToken(td TokenData) (string, error) {
	return m.sign(td, "refresh", m.refreshTTL, m.refreshSecret)
}

func (m *Manager) parse(token string, kind string, secret []byte) (*TokenData, *apperr.AppError) {
	var c claims
	_, err := jwtv5.ParseWithClaims(token, &c, func(t *jwtv5.Token) (any, error) {
		if _, ok := t.Method.(*jwtv5.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return secret, nil
	})
	if err != nil {
		if errors.Is(err, jwtv5.ErrTokenExpired) {
			return nil, apperr.ERRORS.TokenExpired
		}
		if kind == "refresh" {
			return nil, apperr.ERRORS.InvalidRefreshToken
		}
		return nil, apperr.ERRORS.InvalidAuthToken
	}
	if c.Kind != kind {
		if kind == "refresh" {
			return nil, apperr.ERRORS.InvalidRefreshToken
		}
		return nil, apperr.ERRORS.InvalidAuthToken
	}
	td := c.TokenData
	return &td, nil
}

func (m *Manager) DecodeAccessToken(token string) (*TokenData, *apperr.AppError) {
	return m.parse(token, "access", m.accessSecret)
}

func (m *Manager) DecodeRefreshToken(token string) (*TokenData, *apperr.AppError) {
	return m.parse(token, "refresh", m.refreshSecret)
}
