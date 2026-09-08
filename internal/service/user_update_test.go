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

func ptr[T any](v T) *T { return &v }

func mkVerifier(t *testing.T, svc *service.UserService, db *mongo.Database, username, cp string) model.UserView {
	t.Helper()
	in := baseUser(model.RoleVerifier)
	in.Username = username
	in.Email = username + "@ps188.local"
	in.CheckpointID = cp
	v, err := svc.Create(context.Background(), "super-1", "127.0.0.1", in)
	if err != nil {
		t.Fatalf("create verifier: %v", err)
	}
	return v
}

func TestUserService_Update_AdminDisablesVerifierInRegion(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	seedCheckpoint(t, db, "CP-N", "north")
	v := mkVerifier(t, svc, db, "v-n", "CP-N")

	adminNorth := jwt.TokenData{UserID: "admin-n", Role: string(model.RoleAdmin), Region: "north"}
	out, err := svc.Update(context.Background(), adminNorth, "127.0.0.1", v.ID, model.UpdateUserInput{
		Status: ptr(model.UserDisabled),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if out.Status != model.UserDisabled {
		t.Fatalf("status = %q", out.Status)
	}

	// Audit entry written as user.disabled.
	logs, err := repository.NewAuditRepository(db).List(context.Background(), model.AuditFilter{Action: model.ActionUserDisabled}, "", 10)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	if len(logs.Data) != 1 {
		t.Fatalf("want 1 user.disabled audit entry, got %d", len(logs.Data))
	}
}

func TestUserService_Update_AdminCannotTouchOtherRegionOrRole(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	seedCheckpoint(t, db, "CP-N", "north")
	seedCheckpoint(t, db, "CP-S", "south")
	south := mkVerifier(t, svc, db, "v-s", "CP-S")

	adminNorth := jwt.TokenData{UserID: "admin-n", Role: string(model.RoleAdmin), Region: "north"}

	// Different region → forbidden.
	if _, err := svc.Update(context.Background(), adminNorth, "127.0.0.1", south.ID, model.UpdateUserInput{
		Status: ptr(model.UserDisabled),
	}); apperr.From(err).Code != apperr.ERRORS.Forbidden.Code {
		t.Fatalf("want Forbidden, got %v", err)
	}

	// Role change by an admin → forbidden even in-region.
	north := mkVerifier(t, svc, db, "v-n", "CP-N")
	if _, err := svc.Update(context.Background(), adminNorth, "127.0.0.1", north.ID, model.UpdateUserInput{
		Role: ptr(model.RoleAdmin),
	}); apperr.From(err).Code != apperr.ERRORS.Forbidden.Code {
		t.Fatalf("want Forbidden (role change is superadmin-only), got %v", err)
	}
}

func TestUserService_Update_SuperAdminReassignsCheckpoint(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	seedCheckpoint(t, db, "CP-N", "north")
	seedCheckpoint(t, db, "CP-S", "south")
	v := mkVerifier(t, svc, db, "v-n", "CP-N")

	super := jwt.TokenData{UserID: "super-1", Role: string(model.RoleSuperAdmin)}
	out, err := svc.Update(context.Background(), super, "127.0.0.1", v.ID, model.UpdateUserInput{
		CheckpointID: ptr("cp-s"),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if out.CheckpointID != "CP-S" || out.Region != "south" {
		t.Fatalf("reassign failed: %+v", out)
	}
}

func TestUserService_Update_SuperAdminPromotesVerifierToAdmin(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	seedCheckpoint(t, db, "CP-N", "north")
	seedCheckpoint(t, db, "CP-S", "south")
	v := mkVerifier(t, svc, db, "v-n", "CP-N")

	super := jwt.TokenData{UserID: "super-1", Role: string(model.RoleSuperAdmin)}

	// Promote: the verifier's checkpoint-derived region carries over as the
	// admin region, and the checkpoint binding is cleared.
	out, err := svc.Update(context.Background(), super, "127.0.0.1", v.ID, model.UpdateUserInput{
		Role: ptr(model.RoleAdmin),
	})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
	if out.Role != model.RoleAdmin || out.Region != "north" || out.CheckpointID != "" {
		t.Fatalf("promote result = %+v", out)
	}

	// A different region can be set in the same call.
	out2, err := svc.Update(context.Background(), super, "127.0.0.1", v.ID, model.UpdateUserInput{
		Region: ptr("south"),
	})
	if err != nil || out2.Region != "south" {
		t.Fatalf("region reassign: %v / %+v", err, out2)
	}

	logs, _ := repository.NewAuditRepository(db).List(context.Background(), model.AuditFilter{Action: model.ActionUserRoleChanged}, "", 10)
	if len(logs.Data) != 1 {
		t.Fatalf("want 1 user.role_changed entry, got %d", len(logs.Data))
	}
}

func TestUserService_Update_SuperAdminDemoteRequiresCheckpoint(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	seedCheckpoint(t, db, "CP-N", "north")

	in := baseUser(model.RoleAdmin)
	in.Region = "north"
	admin, err := svc.Create(context.Background(), "super-1", "127.0.0.1", in)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	super := jwt.TokenData{UserID: "super-1", Role: string(model.RoleSuperAdmin)}

	// admin -> verifier with no checkpoint → MissingScopeField.
	if _, err := svc.Update(context.Background(), super, "127.0.0.1", admin.ID, model.UpdateUserInput{
		Role: ptr(model.RoleVerifier),
	}); apperr.From(err).Code != apperr.ERRORS.MissingScopeField.Code {
		t.Fatalf("want MissingScopeField, got %v", err)
	}

	// With a checkpoint it succeeds.
	out, err := svc.Update(context.Background(), super, "127.0.0.1", admin.ID, model.UpdateUserInput{
		Role:         ptr(model.RoleVerifier),
		CheckpointID: ptr("CP-N"),
	})
	if err != nil || out.Role != model.RoleVerifier || out.CheckpointID != "CP-N" || out.Region != "north" {
		t.Fatalf("demote: %v / %+v", err, out)
	}
}

func TestUserService_Update_SelfDisableRejected(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := newUserSvc(t, db)
	seedCheckpoint(t, db, "CP-N", "north")

	in := baseUser(model.RoleAdmin)
	in.Region = "north"
	admin, err := svc.Create(context.Background(), "super-1", "127.0.0.1", in)
	if err != nil {
		t.Fatalf("create admin: %v", err)
	}

	selfTok := jwt.TokenData{UserID: admin.ID, Role: string(model.RoleSuperAdmin)}
	if _, err := svc.Update(context.Background(), selfTok, "127.0.0.1", admin.ID, model.UpdateUserInput{
		Status: ptr(model.UserDisabled),
	}); apperr.From(err).Code != apperr.ERRORS.CannotModifySelf.Code {
		t.Fatalf("want CannotModifySelf, got %v", err)
	}
}
