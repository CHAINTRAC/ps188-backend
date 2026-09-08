package repository

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/response"
)

// CheckpointRepository is the only place that touches the checkpoints collection.
type CheckpointRepository interface {
	Create(ctx context.Context, c *model.Checkpoint) (*model.Checkpoint, error)
	FindByCode(ctx context.Context, code string) (*model.Checkpoint, error)
	List(ctx context.Context, f model.CheckpointFilter, cursor string, limit int64) (response.Page[model.CheckpointView], error)
	// Update sets the provided fields on the checkpoint identified by code and
	// returns the updated document.
	Update(ctx context.Context, code string, set bson.M) (*model.Checkpoint, error)
	// RegionExists reports whether at least one checkpoint is registered in region.
	RegionExists(ctx context.Context, region string) (bool, error)
	// Count returns the number of checkpoints, optionally scoped to a region
	// (empty region = all). Used for the super-admin dashboard totals.
	Count(ctx context.Context, region string) (int64, error)
}

type mongoCheckpointRepo struct {
	coll *mongo.Collection
}

func NewCheckpointRepository(db *mongo.Database) CheckpointRepository {
	return &mongoCheckpointRepo{coll: db.Collection(model.CollCheckpoints)}
}

func (r *mongoCheckpointRepo) Create(ctx context.Context, c *model.Checkpoint) (*model.Checkpoint, error) {
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	res, err := r.coll.InsertOne(ctx, c)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, apperr.ERRORS.CheckpointExists.Wrap(err)
		}
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	c.ID = res.InsertedID.(bson.ObjectID)
	return c, nil
}

func (r *mongoCheckpointRepo) FindByCode(ctx context.Context, code string) (*model.Checkpoint, error) {
	var c model.Checkpoint
	err := r.coll.FindOne(ctx, bson.M{"code": model.NormalizeCheckpointCode(code)}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apperr.ERRORS.CheckpointNotFound
	}
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return &c, nil
}

func (r *mongoCheckpointRepo) List(ctx context.Context, f model.CheckpointFilter, cursor string, limit int64) (response.Page[model.CheckpointView], error) {
	var zero response.Page[model.CheckpointView]
	filter := bson.M{}
	if f.Region != "" {
		filter["region"] = f.Region
	}
	if f.AdminID != "" {
		filter["admin_id"] = f.AdminID
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
	var rows []model.Checkpoint
	if err := cur.All(ctx, &rows); err != nil {
		return zero, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return pageOf(rows, limit, func(c model.Checkpoint) model.CheckpointView { return c.View() },
		func(c model.Checkpoint) string { return c.ID.Hex() }), nil
}

func (r *mongoCheckpointRepo) Update(ctx context.Context, code string, set bson.M) (*model.Checkpoint, error) {
	set["updated_at"] = time.Now().UTC()
	var c model.Checkpoint
	err := r.coll.FindOneAndUpdate(ctx,
		bson.M{"code": model.NormalizeCheckpointCode(code)},
		bson.M{"$set": set},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apperr.ERRORS.CheckpointNotFound
	}
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return &c, nil
}

func (r *mongoCheckpointRepo) Count(ctx context.Context, region string) (int64, error) {
	q := bson.M{}
	if region != "" {
		q["region"] = region
	}
	n, err := r.coll.CountDocuments(ctx, q)
	if err != nil {
		return 0, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return n, nil
}

func (r *mongoCheckpointRepo) RegionExists(ctx context.Context, region string) (bool, error) {
	n, err := r.coll.CountDocuments(ctx, bson.M{"region": region}, options.Count().SetLimit(1))
	if err != nil {
		return false, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return n > 0, nil
}
