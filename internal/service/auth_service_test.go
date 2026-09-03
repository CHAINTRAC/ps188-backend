package service_test

import (
	"context"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/testsupport"
)

func TestAuthService_Login(t *testing.T) {
	db := testsupport.RequireMongo(t)
	users := repository.NewUserRepository(db)
	jwtMgr := jwt.NewManager("access-secret", "refresh-secret", time.Hour, time.Hour)
	svc := service.NewAuthService(users, repository.NewAuditRepository(db), jwtMgr)
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("s3cret-password"), bcrypt.MinCost)
	if _, err := users.Create(ctx, &model.User{
		Username: "officer.jane", FullName: "Jane", Email: "jane@ps188.local",
		PasswordHash: string(hash), Role: model.RoleVerifier, Status: model.UserActive,
		Region: "north", CheckpointID: "CP-01",
	}); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	t.Run("success by username", func(t *testing.T) {
		res, err := svc.Login(ctx, "officer.jane", "s3cret-password", "127.0.0.1")
		if err != nil {
			t.Fatalf("login: %v", err)
		}
		if res.Access == "" || res.Refresh == "" || res.User.Username != "officer.jane" {
			t.Fatalf("bad login result: %+v", res)
		}
		td, aerr := jwtMgr.DecodeAccessToken(res.Access)
		if aerr != nil {
			t.Fatalf("access token invalid: %v", aerr)
		}
		if td.Region != "north" || td.CheckpointID != "CP-01" {
			t.Fatalf("token missing scope claims: %+v", td)
		}
	})

	t.Run("success by email", func(t *testing.T) {
		res, err := svc.Login(ctx, "JANE@ps188.local", "s3cret-password", "127.0.0.1")
		if err != nil {
			t.Fatalf("email login: %v", err)
		}
		if res.User.Username != "officer.jane" {
			t.Fatalf("bad user: %+v", res.User)
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		_, err := svc.Login(ctx, "officer.jane", "nope", "127.0.0.1")
		if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.InvalidCredentials.Code {
			t.Fatalf("want InvalidCredentials, got %v", err)
		}
	})

	t.Run("unknown user is indistinguishable", func(t *testing.T) {
		_, err := svc.Login(ctx, "ghost", "whatever", "127.0.0.1")
		if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.InvalidCredentials.Code {
			t.Fatalf("want InvalidCredentials, got %v", err)
		}
	})

	t.Run("writes an auth.login audit entry", func(t *testing.T) {
		n, err := db.Collection(model.CollAuditLogs).CountDocuments(ctx, bson.M{"action": model.ActionAuthLogin})
		if err != nil {
			t.Fatalf("count audit: %v", err)
		}
		if n == 0 {
			t.Fatal("expected at least one auth.login audit entry")
		}
	})
}

func TestAuthService_Refresh(t *testing.T) {
	db := testsupport.RequireMongo(t)
	users := repository.NewUserRepository(db)
	jwtMgr := jwt.NewManager("a", "r", time.Hour, time.Hour)
	svc := service.NewAuthService(users, repository.NewAuditRepository(db), jwtMgr)
	ctx := context.Background()

	hash, _ := bcrypt.GenerateFromPassword([]byte("pw12345678"), bcrypt.MinCost)
	u, err := users.Create(ctx, &model.User{
		Username: "sup.bob", FullName: "Bob", Email: "bob@ps188.local",
		PasswordHash: string(hash), Role: model.RoleVerifier, Status: model.UserActive,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	refresh, _ := jwtMgr.CreateRefreshToken(jwt.TokenData{UserID: u.ID.Hex(), Username: u.Username, Role: string(u.Role)})
	access, err := svc.Refresh(ctx, refresh)
	if err != nil || access == "" {
		t.Fatalf("refresh: %v", err)
	}

	if _, err := svc.Refresh(ctx, "not-a-token"); err == nil {
		t.Fatal("expected error for garbage refresh token")
	}
}
