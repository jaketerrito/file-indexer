package storage

import (
	"context"
	"io"
	"time"
)

// ObjectInfo is metadata about an object without its content.
type ObjectInfo struct {
	Key          string
	Size         int64
	ContentType  string
	LastModified time.Time
}

// Object is a retrieved object together with its content stream. The caller is
// responsible for closing Reader.
type Object struct {
	ObjectInfo
	Reader io.ReadCloser
}

// Storage abstracts an object store. The bucket is bound at construction, so
// methods operate on keys within that single bucket.
type Storage interface {
	// Get retrieves an object and its content stream.
	Get(ctx context.Context, key string) (*Object, error)

	GetURL(ctx context.Context, key string) (string, error)

	// GetInlineURL is GetURL for content meant to be rendered in place (an
	// <img> tag, say) rather than downloaded: it omits the attachment
	// content-disposition that GetURL forces.
	GetInlineURL(ctx context.Context, key string) (string, error)

	// PutURL returns a time-limited presigned URL a client can PUT the
	// object's bytes to directly, without routing them through this service.
	// The client sets its own Content-Type header on the PUT; minio-go's
	// presigned-PUT signing does not bind headers into the signature, so it
	// is not verified here (see server.go for the reference-based indexing
	// path that stats the object afterward instead of trusting the upload).
	PutURL(ctx context.Context, key string) (string, error)

	// Put writes an object. size must be the exact number of bytes readable
	// from r. contentType becomes the object's Content-Type and is what
	// clients receive when fetching it, including via presigned URLs.
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error

	// Walk lists every object in the bucket and invokes fn with the metadata
	// the listing provides (key, size, last-modified; ContentType is empty —
	// S3 listings do not include it, use Stat). Walking stops at the first
	// error from fn.
	Walk(ctx context.Context, fn func(ObjectInfo) error) error

	// Stat returns metadata for a single object without fetching its content.
	Stat(ctx context.Context, key string) (ObjectInfo, error)

	Delete(ctx context.Context, key string) error

	// DeleteMany deletes multiple objects in as few round trips as the
	// underlying client allows (S3's multi-object delete API caps a single
	// request at 1000 keys; batching above that is the implementation's
	// responsibility). Not atomic: on partial failure, DeleteMany returns a
	// combined error for the keys that failed, but keys that succeeded stay
	// deleted. Callers that need to know which specific keys failed should
	// not rely on this — recursive directory delete's contract is "some
	// prefix of the objects may remain, retry or let the crawler reconcile
	// the DB", not per-key granularity.
	DeleteMany(ctx context.Context, keys []string) error
}
