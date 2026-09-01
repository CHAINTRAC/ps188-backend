// Package database owns the MongoDB connection and index setup. Nothing else
// dials Mongo; repositories receive a *mongo.Database.
package database

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"

	"github.com/sih26/ps188-backend/internal/model"
)

// Connect dials Mongo, pings it, and returns the client + the app database.
func Connect(ctx context.Context, uri, dbName string) (*mongo.Client, *mongo.Database, error) {
	opts := options.Client().
		ApplyURI(uri).
		SetServerSelectionTimeout(10 * time.Second).
		SetConnectTimeout(10 * time.Second)

	client, err := mongo.Connect(opts)
	if err != nil {
		return nil, nil, fmt.Errorf("mongo connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, readpref.Primary()); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, nil, fmt.Errorf("mongo ping: %w", err)
	}
	return client, client.Database(dbName), nil
}

// EnsureIndexes creates every index the app relies on. Idempotent — safe to run
// on every boot.
func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	users := []mongo.IndexModel{
		{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_users_username")},
		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_users_email")},
	}
	if _, err := db.Collection(model.CollUsers).Indexes().CreateMany(ctx, users); err != nil {
		return fmt.Errorf("users indexes: %w", err)
	}

	screenings := []mongo.IndexModel{
		{Keys: bson.D{{Key: "reference_no", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uq_screenings_reference_no")},
		{Keys: bson.D{{Key: "status", Value: 1}, {Key: "created_at", Value: -1}}, Options: options.Index().SetName("idx_screenings_status_created")},
		{Keys: bson.D{{Key: "verdict", Value: 1}}, Options: options.Index().SetName("idx_screenings_verdict")},
		{Keys: bson.D{{Key: "checkpoint_id", Value: 1}, {Key: "created_at", Value: -1}}, Options: options.Index().SetName("idx_screenings_checkpoint_created")},
		{Keys: bson.D{{Key: "officer_id", Value: 1}}, Options: options.Index().SetName("idx_screenings_officer")},
	}
	if _, err := db.Collection(model.CollScreenings).Indexes().CreateMany(ctx, screenings); err != nil {
		return fmt.Errorf("screenings indexes: %w", err)
	}

	audit := []mongo.IndexModel{
		{Keys: bson.D{{Key: "created_at", Value: -1}}, Options: options.Index().SetName("idx_audit_created")},
		{Keys: bson.D{{Key: "reference_type", Value: 1}, {Key: "reference_id", Value: 1}}, Options: options.Index().SetName("idx_audit_reference")},
	}
	if _, err := db.Collection(model.CollAuditLogs).Indexes().CreateMany(ctx, audit); err != nil {
		return fmt.Errorf("audit indexes: %w", err)
	}
	return nil
}
