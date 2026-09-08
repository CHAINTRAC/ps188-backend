package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sih26/ps188-backend/internal/apperr"
	"github.com/sih26/ps188-backend/internal/model"
	"github.com/sih26/ps188-backend/internal/response"
)

// ScreeningRepository is the only place that touches the screenings collection.
type ScreeningRepository interface {
	Create(ctx context.Context, s *model.Screening) (*model.Screening, error)
	FindByID(ctx context.Context, id string) (*model.Screening, error)
	List(ctx context.Context, f model.ScreeningFilter, cursor string, limit int64) (response.Page[model.ScreeningView], error)
	SetResult(ctx context.Context, id string, status model.ScreeningStatus, verdict model.Verdict, risk float64, engine *model.EngineResult, failure string) (*model.Screening, error)
	// SetChecks records advisory flags, blacklist matches, and an adjusted risk
	// score raised during post-engine checks, and appends any extra evidence
	// reasons onto engine.reasons. Additive — never clears the engine result or
	// the verdict.
	SetChecks(ctx context.Context, id string, flags []string, matches []model.BlacklistMatch, risk float64, appendReasons []string) (*model.Screening, error)
	SetDecision(ctx context.Context, id string, d model.OfficerDecision) (*model.Screening, error)
	// NextSequence returns a gapless per-day counter used to build reference_no.
	NextSequence(ctx context.Context, day string) (int64, error)
	// Aggregate runs a read-only aggregation pipeline against the screenings
	// collection and decodes the result documents into []bson.M. It is the single
	// escape hatch the analytics service uses for dashboard / report rollups — all
	// pipeline construction lives in service/analytics_service.go, never here.
	Aggregate(ctx context.Context, pipeline mongo.Pipeline) ([]bson.M, error)
}

type mongoScreeningRepo struct {
	coll     *mongo.Collection
	counters *mongo.Collection
}

func NewScreeningRepository(db *mongo.Database) ScreeningRepository {
	return &mongoScreeningRepo{
		coll:     db.Collection(model.CollScreenings),
		counters: db.Collection("counters"),
	}
}

func (r *mongoScreeningRepo) Create(ctx context.Context, s *model.Screening) (*model.Screening, error) {
	now := time.Now().UTC()
	s.CreatedAt, s.UpdatedAt = now, now
	res, err := r.coll.InsertOne(ctx, s)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return nil, apperr.ERRORS.DuplicateResource.Wrap(err)
		}
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	s.ID = res.InsertedID.(bson.ObjectID)
	return s, nil
}

func (r *mongoScreeningRepo) FindByID(ctx context.Context, id string) (*model.Screening, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningNotFound
	}
	var s model.Screening
	err = r.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apperr.ERRORS.ScreeningNotFound
	}
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return &s, nil
}

func (r *mongoScreeningRepo) List(ctx context.Context, f model.ScreeningFilter, cursor string, limit int64) (response.Page[model.ScreeningView], error) {
	var zero response.Page[model.ScreeningView]
	filter := bson.M{}
	if f.Verdict != "" {
		filter["verdict"] = f.Verdict
	}
	if f.DocType != "" {
		filter["doc_type"] = f.DocType
	}
	if f.Status != "" {
		filter["status"] = f.Status
	}
	if f.CheckpointID != "" {
		filter["checkpoint_id"] = f.CheckpointID
	}
	if f.OfficerID != "" {
		filter["officer_id"] = f.OfficerID
	}
	if f.Region != "" {
		filter["region"] = f.Region
	}
	if f.Decided != nil {
		filter["officer_decision"] = bson.M{"$exists": *f.Decided}
	}
	if f.DecisionValue != "" {
		filter["officer_decision.decision"] = f.DecisionValue
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
	var rows []model.Screening
	if err := cur.All(ctx, &rows); err != nil {
		return zero, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return pageOf(rows, limit, func(s model.Screening) model.ScreeningView { return s.View() },
		func(s model.Screening) string { return s.ID.Hex() }), nil
}

func (r *mongoScreeningRepo) SetResult(ctx context.Context, id string, status model.ScreeningStatus, verdict model.Verdict, risk float64, engine *model.EngineResult, failure string) (*model.Screening, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningNotFound
	}
	set := bson.M{
		"status":         status,
		"verdict":        verdict,
		"risk_score":     risk,
		"failure_reason": failure,
		"updated_at":     time.Now().UTC(),
	}
	if engine != nil {
		set["engine"] = engine
	}
	return r.findOneAndUpdate(ctx, oid, bson.M{"$set": set})
}

func (r *mongoScreeningRepo) SetChecks(ctx context.Context, id string, flags []string, matches []model.BlacklistMatch, risk float64, appendReasons []string) (*model.Screening, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningNotFound
	}
	set := bson.M{
		"flags":      flags,
		"risk_score": risk,
		"updated_at": time.Now().UTC(),
	}
	if matches != nil {
		set["blacklist_matches"] = matches
	}
	update := bson.M{"$set": set}
	if len(appendReasons) > 0 {
		update["$push"] = bson.M{"engine.reasons": bson.M{"$each": appendReasons}}
	}
	return r.findOneAndUpdate(ctx, oid, update)
}

func (r *mongoScreeningRepo) SetDecision(ctx context.Context, id string, d model.OfficerDecision) (*model.Screening, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, apperr.ERRORS.ScreeningNotFound
	}
	// The nil-guard on officer_decision makes this a single-writer operation:
	// a second concurrent decision matches zero documents and returns not-found,
	// which the service maps to AlreadyDecided.
	filter := bson.M{"_id": oid, "officer_decision": bson.M{"$exists": false}}
	update := bson.M{"$set": bson.M{"officer_decision": d, "updated_at": time.Now().UTC()}}
	after := options.After
	var s model.Screening
	err = r.coll.FindOneAndUpdate(ctx, filter, update,
		options.FindOneAndUpdate().SetReturnDocument(after)).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, mongo.ErrNoDocuments
	}
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return &s, nil
}

func (r *mongoScreeningRepo) findOneAndUpdate(ctx context.Context, oid bson.ObjectID, update bson.M) (*model.Screening, error) {
	var s model.Screening
	err := r.coll.FindOneAndUpdate(ctx, bson.M{"_id": oid}, update,
		options.FindOneAndUpdate().SetReturnDocument(options.After)).Decode(&s)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, apperr.ERRORS.ScreeningNotFound
	}
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return &s, nil
}

func (r *mongoScreeningRepo) Aggregate(ctx context.Context, pipeline mongo.Pipeline) ([]bson.M, error) {
	cur, err := r.coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	var out []bson.M
	if err := cur.All(ctx, &out); err != nil {
		return nil, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return out, nil
}

func (r *mongoScreeningRepo) NextSequence(ctx context.Context, day string) (int64, error) {
	var out struct {
		Seq int64 `bson:"seq"`
	}
	err := r.counters.FindOneAndUpdate(ctx,
		bson.M{"_id": fmt.Sprintf("screening:%s", day)},
		bson.M{"$inc": bson.M{"seq": int64(1)}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&out)
	if err != nil {
		return 0, apperr.ERRORS.DatabaseError.Wrap(err)
	}
	return out.Seq, nil
}
