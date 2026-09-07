package service

import (
	"context"

	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/repository"
	"github.com/sih26/ps188-backend/internal/response"
)

// AuditService is a thin read layer over the append-only audit repository.
type AuditService struct {
	repo repository.AuditRepository
}

func NewAuditService(repo repository.AuditRepository) *AuditService {
	return &AuditService{repo: repo}
}

func (s *AuditService) List(ctx context.Context, f model.AuditFilter, cursor string, limit int64) (response.Page[model.AuditLogView], error) {
	return s.repo.List(ctx, f, cursor, limit)
}
