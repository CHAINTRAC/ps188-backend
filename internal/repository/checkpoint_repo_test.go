package repository_test

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/testsupport"
)

func TestCheckpointRepo_CreateFindListUpdate(t *testing.T) {
	db := testsupport.RequireMongo(t)
	repo := repository.NewCheckpointRepository(db)
	ctx := context.Background()

	if _, err := repo.Create(ctx, &model.Checkpoint{Code: "CP-01", Region: "north", Status: model.CheckpointActive}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := repo.Create(ctx, &model.Checkpoint{Code: "CP-02", Region: "south", Status: model.CheckpointActive}); err != nil {
		t.Fatalf("create 2: %v", err)
	}

	// Duplicate code is rejected.
	_, err := repo.Create(ctx, &model.Checkpoint{Code: "CP-01", Region: "north", Status: model.CheckpointActive})
	if ae := apperr.From(err); ae == nil || ae.Code != apperr.ERRORS.CheckpointExists.Code {
		t.Fatalf("want CheckpointExists, got %v", err)
	}

	// Find by code is case-insensitive.
	cp, err := repo.FindByCode(ctx, "cp-01")
	if err != nil || cp.Region != "north" {
		t.Fatalf("find: %v / %+v", err, cp)
	}

	// Not found.
	if _, err := repo.FindByCode(ctx, "CP-99"); apperr.From(err).Code != apperr.ERRORS.CheckpointNotFound.Code {
		t.Fatalf("want CheckpointNotFound, got %v", err)
	}

	// List filtered by region.
	page, err := repo.List(ctx, model.CheckpointFilter{Region: "north"}, "", 20)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Data) != 1 || page.Data[0].Code != "CP-01" {
		t.Fatalf("region filter = %+v", page.Data)
	}

	// RegionExists.
	if ok, _ := repo.RegionExists(ctx, "south"); !ok {
		t.Fatal("expected south region to exist")
	}
	if ok, _ := repo.RegionExists(ctx, "east"); ok {
		t.Fatal("did not expect east region")
	}

	// Update status + admin.
	updated, err := repo.Update(ctx, "CP-01", bson.M{"status": model.CheckpointAttention, "admin_id": "admin-1"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Status != model.CheckpointAttention || updated.AdminID != "admin-1" {
		t.Fatalf("update result = %+v", updated)
	}
}
