package service

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/response"
)

// CheckpointService owns the checkpoint registry: super-admin CRUD plus the
// Resolve/RegionExists lookups other services use to scope users and screenings.
type CheckpointService struct {
	repo  repository.CheckpointRepository
	audit repository.AuditRepository
}

func NewCheckpointService(repo repository.CheckpointRepository, audit repository.AuditRepository) *CheckpointService {
	return &CheckpointService{repo: repo, audit: audit}
}

func (s *CheckpointService) Create(ctx context.Context, actorID, ip string, in model.CreateCheckpointInput) (model.CheckpointView, error) {
	var zero model.CheckpointView
	cp := &model.Checkpoint{
		Code:    model.NormalizeCheckpointCode(in.Code),
		Region:  in.Region,
		AdminID: in.AdminID,
		Status:  model.CheckpointActive,
	}
	if cp.Code == "" || cp.Region == "" {
		return zero, apperr.ERRORS.ValidationError
	}

	created, err := s.repo.Create(ctx, cp)
	if err != nil {
		return zero, err
	}

	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        actorID,
		Action:        model.ActionCheckpointCreated,
		ReferenceType: "checkpoint",
		ReferenceID:   created.Code,
		NewData:       bson.M{"code": created.Code, "region": created.Region, "admin_id": created.AdminID},
		IPAddress:     ip,
		CreatedAt:     time.Now().UTC(),
	})
	return created.View(), nil
}

func (s *CheckpointService) Get(ctx context.Context, code string) (model.CheckpointView, error) {
	cp, err := s.repo.FindByCode(ctx, code)
	if err != nil {
		return model.CheckpointView{}, err
	}
	return cp.View(), nil
}

func (s *CheckpointService) List(ctx context.Context, f model.CheckpointFilter, cursor string, limit int64) (response.Page[model.CheckpointView], error) {
	return s.repo.List(ctx, f, cursor, limit)
}

func (s *CheckpointService) Update(ctx context.Context, actorID, ip, code string, in model.UpdateCheckpointInput) (model.CheckpointView, error) {
	var zero model.CheckpointView
	before, err := s.repo.FindByCode(ctx, code)
	if err != nil {
		return zero, err
	}

	set := bson.M{}
	if in.AdminID != nil {
		set["admin_id"] = *in.AdminID
	}
	if in.Status != nil {
		if !in.Status.Valid() {
			return zero, apperr.ERRORS.InvalidCheckpointStatus
		}
		set["status"] = *in.Status
	}
	if len(set) == 0 {
		return before.View(), nil
	}

	updated, err := s.repo.Update(ctx, code, set)
	if err != nil {
		return zero, err
	}

	_ = s.audit.Insert(ctx, model.AuditLog{
		UserID:        actorID,
		Action:        model.ActionCheckpointUpdated,
		ReferenceType: "checkpoint",
		ReferenceID:   updated.Code,
		OldData:       bson.M{"admin_id": before.AdminID, "status": before.Status},
		NewData:       bson.M{"admin_id": updated.AdminID, "status": updated.Status},
		IPAddress:     ip,
		CreatedAt:     time.Now().UTC(),
	})
	return updated.View(), nil
}

// Resolve returns the region a checkpoint belongs to. Used by user create
// (verifier) and screening submit to denormalise the region.
func (s *CheckpointService) Resolve(ctx context.Context, code string) (string, error) {
	cp, err := s.repo.FindByCode(ctx, code)
	if err != nil {
		return "", err
	}
	return cp.Region, nil
}

// RegionExists reports whether any checkpoint is registered in region — used to
// validate an admin's region against the registry.
func (s *CheckpointService) RegionExists(ctx context.Context, region string) (bool, error) {
	return s.repo.RegionExists(ctx, region)
}
