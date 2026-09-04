package storage_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/sih26/ps188-backend/internal/storage"
)

func TestLocalFileStore_PutGetDelete(t *testing.T) {
	store, err := storage.NewLocalFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	ctx := context.Background()
	want := []byte("fake jpeg bytes")

	id, err := store.Put(ctx, "passport.jpg", bytes.NewReader(want))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	// The id must be a valid bson.ObjectID hex string — Screening.ImageFileID
	// round-trips the FileStore id through bson.ObjectIDFromHex.
	if _, err := bson.ObjectIDFromHex(id); err != nil {
		t.Fatalf("id %q is not a valid ObjectID hex: %v", id, err)
	}

	var got bytes.Buffer
	if err := store.Get(ctx, id, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("got %q, want %q", got.Bytes(), want)
	}

	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := store.Get(ctx, id, &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error reading a deleted file")
	}

	// Deleting an already-missing (or never-existing) id is not an error.
	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("delete missing: %v", err)
	}
	if err := store.Delete(ctx, bson.NewObjectID().Hex()); err != nil {
		t.Fatalf("delete never-existed: %v", err)
	}
}

func TestLocalFileStore_CreatesBaseDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "uploads")
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("dir should not exist yet")
	}
	if _, err := storage.NewLocalFileStore(dir); err != nil {
		t.Fatalf("new store: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("base dir not created: %v", err)
	}
}
