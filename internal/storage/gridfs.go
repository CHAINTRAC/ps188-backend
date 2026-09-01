package storage

import (
	"context"
	"errors"
	"io"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// gridFSStore keeps document images in a MongoDB GridFS bucket.
type gridFSStore struct {
	bucket *mongo.GridFSBucket
}

// NewGridFS returns a FileStore backed by the "documents" GridFS bucket in db.
func NewGridFS(db *mongo.Database) FileStore {
	return &gridFSStore{bucket: db.GridFSBucket()}
}

func (s *gridFSStore) Put(ctx context.Context, filename string, r io.Reader) (string, error) {
	id, err := s.bucket.UploadFromStream(ctx, filename, r)
	if err != nil {
		return "", err
	}
	return id.Hex(), nil
}

func (s *gridFSStore) Get(ctx context.Context, id string, w io.Writer) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = s.bucket.DownloadToStream(ctx, oid, w)
	return err
}

func (s *gridFSStore) Delete(ctx context.Context, id string) error {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	err = s.bucket.Delete(ctx, oid)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil
	}
	return err
}
