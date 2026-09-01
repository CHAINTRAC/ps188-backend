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

// UserRepository is the only place that touches the users collection.
type UserRepository interface {
	Create(ctx context.Context, u *model.User) (*model.User, error)
	FindByID(ctx context.Context, id string) (*model.User, error)
	FindByUsername(ctx context.Context, username string) (*model.User, error)
	List(ctx context.Context, cursor string, limit int64) (response.Page[model.UserView], error)
	Count(ctx context.Context) (int64, error)
}

type mongoUserRepo struct {
	coll *mongo.Collection
}

func NewUserRepository(db *mongo.Database) UserRepository {
	return &mongoUserRepo{coll: db.Collection(model.CollUsers)}
}

func (r *mongoUserRepo) Create(ctx context.Context, u *model.User) (*model.User, error) {
	now := time.Now().UTC()
	u.CreatedAt, u.UpdatedAt = now, now
	res, err := r.coll.InsertOne(ctx, u)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, apperr.ERRORS.DuplicateResource.Wrap(err)
		}
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	u.ID = res.InsertedID.(bson.ObjectID)
	return u, nil
}

func (r *mongoUserRepo) FindByID(ctx context.Context, id string) (*model.User, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperr.ERRORS.UserNotFound
	}
	var u model.User
	err = r.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apperr.ERRORS.UserNotFound
	}
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return &u, nil
}

func (r *mongoUserRepo) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	var u model.User
	err := r.coll.FindOne(ctx, bson.M{"username": username}).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apperr.ERRORS.UserNotFound
	}
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return &u, nil
}

func (r *mongoUserRepo) List(ctx context.Context, cursor string, limit int64) (response.Page[model.UserView], error) {
	var zero response.Page[model.UserView]
	filter := bson.M{}
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
	var rows []model.User
	if err := cur.All(ctx, &rows); err != nil {
		return zero, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return pageOf(rows, limit, func(u model.User) model.UserView { return u.View() },
		func(u model.User) string { return u.ID.Hex() }), nil
}

func (r *mongoUserRepo) Count(ctx context.Context) (int64, error) {
	n, err := r.coll.CountDocuments(ctx, bson.M{})
	if err != nil {
		return 0, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return n, nil
}
