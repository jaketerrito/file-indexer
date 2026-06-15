package storage

import (
	"context"
	"file-indexer/internal/pb"
	"io"
	"time"
)

// Object is a retrieved object together with its content stream. The caller is
// responsible for closing Reader.
type Object struct {
	Reader      io.ReadCloser
	Size        int64
	ContentType string
	ETag        string
}

// ObjectInfo is metadata about an object without its content.
type ObjectInfo struct {
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
}

// Storage abstracts an object store. The bucket is bound at construction, so
// methods operate on keys within that single bucket.
type Storage interface {
	// Get retrieves an object and its content stream.
	Get(ctx context.Context, key string) (*Object, error)

	// Put stores an object from r. size may be -1 if unknown.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error

	Walk(ctx context.Context, fn func(*pb.FileRef) error) error

	// Stat returns metadata for a single object without fetching its content.
	Stat(ctx context.Context, key string) (ObjectInfo, error)
}
