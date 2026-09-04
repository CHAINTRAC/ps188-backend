package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// localFileStore keeps document images as plain files on local disk, rooted at
// baseDir. Ids are bson.ObjectID hex strings — the same shape GridFS handed out —
// so callers that round-trip the id through bson.ObjectIDFromHex (Screening's
// ImageFileID field) keep working unchanged regardless of which FileStore is wired.
type localFileStore struct {
	baseDir string
}

// NewLocalFileStore returns a FileStore that writes document images to baseDir
// on the local filesystem, creating the directory if it doesn't exist yet.
func NewLocalFileStore(baseDir string) (FileStore, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("local storage: create %s: %w", baseDir, err)
	}
	return &localFileStore{baseDir: baseDir}, nil
}

// path resolves id to its on-disk location. id always comes from Put (this
// store's own bson.ObjectID hex) or from a Screening.ImageFileID persisted from
// one, so it is never attacker-controlled path input.
func (s *localFileStore) path(id string) string {
	return filepath.Join(s.baseDir, id)
}

func (s *localFileStore) Put(ctx context.Context, filename string, r io.Reader) (string, error) {
	id := bson.NewObjectID().Hex()
	f, err := os.Create(s.path(id))
	if err != nil {
		return "", fmt.Errorf("local storage: create file: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = os.Remove(s.path(id))
		return "", fmt.Errorf("local storage: write file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(s.path(id))
		return "", fmt.Errorf("local storage: close file: %w", err)
	}
	return id, nil
}

func (s *localFileStore) Get(ctx context.Context, id string, w io.Writer) error {
	f, err := os.Open(s.path(id))
	if err != nil {
		return fmt.Errorf("local storage: open file: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("local storage: read file: %w", err)
	}
	return nil
}

func (s *localFileStore) Delete(ctx context.Context, id string) error {
	if err := os.Remove(s.path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("local storage: delete file: %w", err)
	}
	return nil
}
