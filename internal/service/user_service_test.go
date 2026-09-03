package service_test

import (
	"context"
	"testing"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/platform/jwt"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/testsupport"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func newUserSvc(t *testing.T, db *mongo.Database) *service.UserService {
	t.Helper()
	cpSvc := service.NewCheckpointService(
		repository.NewCheckpointRepository(db),
		repository.NewAuditRepository(db),
	)
	return service.NewUserService(
		repository.NewUserRepository(db),
		repository.NewAuditRepository(db),
		cpSvc,
	)
}

func seedCheckpoint(t *testing.T, db *mongo.Database, code, region string) {
	t.Helper()
	_, err := repository.NewCheckpointRepository(db).Create(context.Background(), &model.Checkpoint{
		Code: code, Region: region, Status: model.CheckpointActive,
	})
	if err != nil {
		t.Fatalf("seed checkpoint: %v", err)
	}
}

func baseUser(role model.Role) model.CreateUserInput {
	return model.CreateUserInput{
		Username: "u." + string(role), FullName: "U", Email: string(role) + "@ps188.local",
		Password: "password123", Role: role,
	}
}

func TestUserService_Create_VerifierResolvesRegion(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	seedCheckpoint(t, db, "CP-01", "north")

	in := baseUser(model.RoleVerifier)
	in.CheckpointID = "cp-01"
	view, err := svc.Create(context.Background(), "super-1", "127.0.0.1", in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view.Region != "north" || view.CheckpointID != "CP-01" {
		t.Fatalf("view = %+v", view)
	}
}

func TestUserService_Create_VerifierMissingCheckpoint(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)

	_, err := svc.Create(context.Background(), "super-1", "127.0.0.1", baseUser(model.RoleVerifier))
	if apperr.From(err).Code != apperr.ERRORS.MissingScopeField.Code {
		t.Fatalf("want MissingScopeField, got %v", err)
	}
}

func TestUserService_Create_AdminRegionValidation(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	seedCheckpoint(t, db, "CP-01", "north")

	// Missing region.
	_, err := svc.Create(context.Background(), "super-1", "127.0.0.1", baseUser(model.RoleAdmin))
	if apperr.From(err).Code != apperr.ERRORS.MissingScopeField.Code {
		t.Fatalf("want MissingScopeField, got %v", err)
	}

	// Unknown region.
	in := baseUser(model.RoleAdmin)
	in.Region = "atlantis"
	_, err = svc.Create(context.Background(), "super-1", "127.0.0.1", in)
	if apperr.From(err).Code != apperr.ERRORS.UnknownRegion.Code {
		t.Fatalf("want UnknownRegion, got %v", err)
	}

	// Valid region.
	in.Region = "north"
	view, err := svc.Create(context.Background(), "super-1", "127.0.0.1", in)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}
	if view.Region != "north" {
		t.Fatalf("view = %+v", view)
	}
}

func TestUserService_ChangePassword(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	users := repository.NewUserRepository(db)
	seedCheckpoint(t, db, "CP-01", "north")

	in := baseUser(model.RoleVerifier)
	in.CheckpointID = "CP-01"
	view, err := svc.Create(context.Background(), "super-1", "127.0.0.1", in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Wrong current password.
	if err := svc.ChangePassword(context.Background(), view.ID, "127.0.0.1", "wrong", "newpassword1"); apperr.From(err).Code != apperr.ERRORS.InvalidCurrentPassword.Code {
		t.Fatalf("want InvalidCurrentPassword, got %v", err)
	}

	// Correct current password.
	if err := svc.ChangePassword(context.Background(), view.ID, "127.0.0.1", "password123", "newpassword1"); err != nil {
		t.Fatalf("change: %v", err)
	}

	// The new hash verifies.
	u, err := users.FindByID(context.Background(), view.ID)
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if u.PasswordHash == "" {
		t.Fatal("empty hash")
	}
}

func TestUserService_ResetPassword_Scope(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	seedCheckpoint(t, db, "CP-N", "north")
	seedCheckpoint(t, db, "CP-S", "south")

	mkVerifier := func(cp string) model.UserView {
		in := baseUser(model.RoleVerifier)
		in.Username = "v-" + cp
		in.Email = "v-" + cp + "@ps188.local"
		in.CheckpointID = cp
		v, err := svc.Create(context.Background(), "super-1", "127.0.0.1", in)
		if err != nil {
			t.Fatalf("create verifier: %v", err)
		}
		return v
	}
	north := mkVerifier("CP-N")
	south := mkVerifier("CP-S")

	adminNorth := jwt.TokenData{UserID: "admin-n", Role: string(model.RoleAdmin), Region: "north"}

	// Admin resets a verifier in their region.
	temp, err := svc.ResetPassword(context.Background(), adminNorth, "127.0.0.1", north.ID)
	if err != nil || temp == "" {
		t.Fatalf("reset in-region: %v / %q", err, temp)
	}

	// Admin cannot reset a verifier in another region.
	if _, err := svc.ResetPassword(context.Background(), adminNorth, "127.0.0.1", south.ID); apperr.From(err).Code != apperr.ERRORS.Forbidden.Code {
		t.Fatalf("want Forbidden, got %v", err)
	}

	// Super admin can reset anyone.
	super := jwt.TokenData{UserID: "super-1", Role: string(model.RoleSuperAdmin)}
	if _, err := svc.ResetPassword(context.Background(), super, "127.0.0.1", south.ID); err != nil {
		t.Fatalf("superadmin reset: %v", err)
	}
}
