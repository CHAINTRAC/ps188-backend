package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
)

// AuditRepository is append-only by design: it exposes Insert and nothing that
// updates or deletes a log entry.
type AuditRepository interface {
	Insert(ctx context.Context, entry model.AuditLog) error
}

type mongoAuditRepo struct {
	coll *mongo.Collection
}

func NewAuditRepository(db *mongo.Database) AuditRepository {
	return &mongoAuditRepo{coll: db.Collection(model.CollAuditLogs)}
}

func (r *mongoAuditRepo) Insert(ctx context.Context, entry model.AuditLog) error {
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if _, err := r.coll.InsertOne(ctx, entry); err != nil {
		return apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return nil
}
