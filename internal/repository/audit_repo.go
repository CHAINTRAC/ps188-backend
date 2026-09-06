package repository

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/response"
)

// AuditRepository is append-only — List included, nothing updates or deletes.
type AuditRepository interface {
	Insert(ctx context.Context, entry model.AuditLog) error
	List(ctx context.Context, f model.AuditFilter, cursor string, limit int64) (response.Page[model.AuditLogView], error)
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

func (r *mongoAuditRepo) List(ctx context.Context, f model.AuditFilter, cursor string, limit int64) (response.Page[model.AuditLogView], error) {
	var zero response.Page[model.AuditLogView]
	filter := bson.M{}
	if f.UserID != "" {
		filter["user_id"] = f.UserID
	}
	if f.Action != "" {
		filter["action"] = f.Action
	}
	if f.Region != "" {
		filter["region"] = f.Region
	}
	if cursor != "" {
		oid, err := bson.ObjectIDFromHex(cursor)
		if err != nil {
			return zero, apperr.ERRORS.InvalidQueryParam
		}
		filter["_id"] = bson.M{"$lt": oid}
	}

	opts := options.Find().SetSort(bson.D{{Key: "_id", Value: -1}}).SetLimit(limit + 1)
	cur, err := r.coll.Find(ctx, filter, opts)
	if err != nil {
		return zero, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	var rows []model.AuditLog
	if err := cur.All(ctx, &rows); err != nil {
		return zero, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return pageOf(rows, limit, func(a model.AuditLog) model.AuditLogView { return a.View() },
		func(a model.AuditLog) string { return a.ID.Hex() }), nil
}
