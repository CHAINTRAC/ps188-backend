package service_test

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/service"
	"github.com/sih26/ps188-backend/internal/testsupport"
)

func TestCheckpointService_CRUD_ResolveAndAudit(t *testing.T) {
	db := testsupport.RequireMongo(t)
	svc := service.NewCheckpointService(
		repository.NewCheckpointRepository(db),
		repository.NewAuditRepository(db),
	)
	ctx := context.Background()

	view, err := svc.Create(ctx, "super-1", "127.0.0.1", model.CreateCheckpointInput{Code: "cp-04", Region: "west"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if view.Code != "CP-04" || view.Region != "west" || view.Status != model.CheckpointActive {
		t.Fatalf("view = %+v", view)
	}

	// Audit written.
	n, _ := db.Collection(model.CollAuditLogs).CountDocuments(ctx, bson.M{"action": model.ActionCheckpointCreated})
	if n != 1 {
		t.Fatalf("expected 1 checkpoint.created audit, got %d", n)
	}

	// Resolve returns the region.
	region, err := svc.Resolve(ctx, "CP-04")
	if err != nil || region != "west" {
		t.Fatalf("resolve: %v / %q", err, region)
	}

	// RegionExists.
	if ok, _ := svc.RegionExists(ctx, "west"); !ok {
		t.Fatal("expected west to exist")
	}

	// Update: invalid status rejected.
	bad := model.CheckpointStatus("broken")
	if _, err := svc.Update(ctx, "super-1", "127.0.0.1", "CP-04", model.UpdateCheckpointInput{Status: &bad}); apperr.From(err).Code != apperr.ERRORS.InvalidCheckpointStatus.Code {
		t.Fatalf("want InvalidCheckpointStatus, got %v", err)
	}

	// Update: assign admin + status.
	admin := "admin-9"
	att := model.CheckpointAttention
	upd, err := svc.Update(ctx, "super-1", "127.0.0.1", "CP-04", model.UpdateCheckpointInput{AdminID: &admin, Status: &att})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if upd.AdminID != "admin-9" || upd.Status != model.CheckpointAttention {
		t.Fatalf("update result = %+v", upd)
	}

	// Resolve on a missing checkpoint.
	if _, err := svc.Resolve(ctx, "CP-XX"); apperr.From(err).Code != apperr.ERRORS.CheckpointNotFound.Code {
		t.Fatalf("want CheckpointNotFound, got %v", err)
	}
}
