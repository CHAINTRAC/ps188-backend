// Package testsupport holds shared test helpers. It follows the same real-DB
// philosophy as the reference project: tests run against a real MongoDB (the one
// from docker-compose), each test file provisioning its own uniquely-named
// database and dropping it on cleanup. Nothing here mocks a repository.
package testsupport

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/sih26/ps188-backend/internal/database"
)

// MongoURI is the connection string tests dial. Override with TEST_MONGO_URI.
func MongoURI() string {
	if v := os.Getenv("TEST_MONGO_URI"); v != "" {
		return v
	}
	return "mongodb://localhost:27017"
}

// RequireMongo connects to the test MongoDB and returns a fresh, uniquely-named
// database. The database (and the connection) are torn down in t.Cleanup. If no
// MongoDB is reachable the test is skipped, not failed.
func RequireMongo(t *testing.T) *mongo.Database {
	t.Helper()

	client, err := mongo.Connect(options.Client().
		ApplyURI(MongoURI()).
		SetServerSelectionTimeout(3 * time.Second))
	if err != nil {
		t.Skipf("skipping: cannot connect to test MongoDB (%v)", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Skipf("skipping: test MongoDB not reachable at %s (%v)", MongoURI(), err)
	}

	name := fmt.Sprintf("ps188_test_%d", time.Now().UnixNano())
	db := client.Database(name)

	if err := database.EnsureIndexes(context.Background(), db); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("ensure indexes: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	})
	return db
}
