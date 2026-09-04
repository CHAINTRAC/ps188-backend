// Package storage abstracts binary blob storage for document images.
// STORAGE_DRIVER selects the implementation: "local" (local.go — plain files on
// disk, the current default) or "gridfs" (gridfs.go — same MongoDB, no shared
// filesystem volume needed). Both hand out bson.ObjectID hex ids so they're
// interchangeable without touching callers; an S3 impl can satisfy the same
// interface later.
package storage

import (
	"context"
	"io"
)

// FileStore stores and retrieves document images by an opaque id.
type FileStore interface {
	// Put streams r into storage and returns the new blob id (hex).
	Put(ctx context.Context, filename string, r io.Reader) (string, error)
	// Get streams the blob with id into w.
	Get(ctx context.Context, id string, w io.Writer) error
	// Delete removes the blob. Missing blob is not an error.
	Delete(ctx context.Context, id string) error
}
