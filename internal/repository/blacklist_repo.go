package repository

import (
	"context"
	"errors"
	"regexp"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/response"
)

// BlacklistRepository is the only place that touches the blacklist collection.
type BlacklistRepository interface {
	Create(ctx context.Context, e *model.BlacklistEntry) (*model.BlacklistEntry, error)
	FindByID(ctx context.Context, id string) (*model.BlacklistEntry, error)
	List(ctx context.Context, f model.BlacklistFilter, cursor string, limit int64) (response.Page[model.BlacklistView], error)
	// LookupByDocNumber returns active document-kind entries matching a
	// normalised document number.
	LookupByDocNumber(ctx context.Context, docNumber string) ([]model.BlacklistEntry, error)
	// LookupByIdentity returns active identity-kind entries matching a
	// normalised name plus (when provided) date of birth and nationality.
	LookupByIdentity(ctx context.Context, name, dob, nationality string) ([]model.BlacklistEntry, error)
	// Deactivate flips active=false and returns the updated entry.
	Deactivate(ctx context.Context, id string) (*model.BlacklistEntry, error)
}

type mongoBlacklistRepo struct {
	coll *mongo.Collection
}

func NewBlacklistRepository(db *mongo.Database) BlacklistRepository {
	return &mongoBlacklistRepo{coll: db.Collection(model.CollBlacklist)}
}

func (r *mongoBlacklistRepo) Create(ctx context.Context, e *model.BlacklistEntry) (*model.BlacklistEntry, error) {
	now := time.Now().UTC()
	e.CreatedAt, e.UpdatedAt = now, now
	res, err := r.coll.InsertOne(ctx, e)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, apperr.ERRORS.DuplicateResource.Wrap(err)
		}
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	e.ID = res.InsertedID.(bson.ObjectID)
	return e, nil
}

func (r *mongoBlacklistRepo) FindByID(ctx context.Context, id string) (*model.BlacklistEntry, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperr.ERRORS.BlacklistEntryNotFound
	}
	var e model.BlacklistEntry
	err = r.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apperr.ERRORS.BlacklistEntryNotFound
	}
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return &e, nil
}

func (r *mongoBlacklistRepo) List(ctx context.Context, f model.BlacklistFilter, cursor string, limit int64) (response.Page[model.BlacklistView], error) {
	var zero response.Page[model.BlacklistView]
	filter := bson.M{}
	if f.Kind != "" {
		filter["kind"] = f.Kind
	}
	if f.DocType != "" {
		filter["doc_type"] = f.DocType
	}
	if f.Active != nil {
		filter["active"] = *f.Active
	}
	if q := f.Query; q != "" {
		rx := bson.M{"$regex": regexp.QuoteMeta(q), "$options": "i"}
		filter["$or"] = bson.A{
			bson.M{"doc_number": rx},
			bson.M{"name": rx},
			bson.M{"reason": rx},
		}
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
	var rows []model.BlacklistEntry
	if err := cur.All(ctx, &rows); err != nil {
		return zero, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return pageOf(rows, limit, func(e model.BlacklistEntry) model.BlacklistView { return e.View() },
		func(e model.BlacklistEntry) string { return e.ID.Hex() }), nil
}

func (r *mongoBlacklistRepo) LookupByDocNumber(ctx context.Context, docNumber string) ([]model.BlacklistEntry, error) {
	key := model.NormalizeBlacklistKey(docNumber)
	if key == "" {
		return nil, nil
	}
	return r.find(ctx, bson.M{
		"kind":       model.BlacklistDocument,
		"active":     true,
		"doc_number": key,
	})
}

func (r *mongoBlacklistRepo) LookupByIdentity(ctx context.Context, name, dob, nationality string) ([]model.BlacklistEntry, error) {
	key := model.NormalizeBlacklistKey(name)
	if key == "" {
		return nil, nil
	}
	filter := bson.M{
		"kind":   model.BlacklistIdentity,
		"active": true,
		"name":   key,
	}
	// DOB and nationality tighten the match only when the caller supplies them;
	// a blank stored value also matches (name-only blacklisting).
	if d := model.NormalizeBlacklistKey(dob); d != "" {
		filter["dob"] = bson.M{"$in": bson.A{d, ""}}
	}
	if n := model.NormalizeBlacklistKey(nationality); n != "" {
		filter["nationality"] = bson.M{"$in": bson.A{n, ""}}
	}
	return r.find(ctx, filter)
}

func (r *mongoBlacklistRepo) find(ctx context.Context, filter bson.M) ([]model.BlacklistEntry, error) {
	cur, err := r.coll.Find(ctx, filter)
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	var rows []model.BlacklistEntry
	if err := cur.All(ctx, &rows); err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return rows, nil
}

func (r *mongoBlacklistRepo) Deactivate(ctx context.Context, id string) (*model.BlacklistEntry, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperr.ERRORS.BlacklistEntryNotFound
	}
	var e model.BlacklistEntry
	err = r.coll.FindOneAndUpdate(ctx,
		bson.M{"_id": oid},
		bson.M{"$set": bson.M{"active": false, "updated_at": time.Now().UTC()}},
		options.FindOneAndUpdate().SetReturnDocument(options.After),
	).Decode(&e)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apperr.ERRORS.BlacklistEntryNotFound
	}
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return &e, nil
}
