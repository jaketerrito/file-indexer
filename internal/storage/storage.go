package storage

import (
	"context"
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

// Storage abstracts an object store. Bucket is passed per call to match the
// reference-based indexing model where each request carries its own bucket.
type Storage interface {
	// Get retrieves an object and its content stream.
	Get(ctx context.Context, bucket, key string) (*Object, error)

	// Put stores an object from r. size may be -1 if unknown.
	Put(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) error

	// List returns metadata for objects under prefix.
	List(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)

	// Stat returns metadata for a single object without fetching its content.
	Stat(ctx context.Context, bucket, key string) (ObjectInfo, error)
}
