package storage

import (
	"context"
	"errors"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// errNotImplemented is returned by stubbed methods until they are filled in.
var errNotImplemented = errors.New("storage: not implemented")

// s3Storage is the S3-backed implementation of Storage. It uses the minio-go
// client, which speaks the S3 API and works against any S3-compatible store
// (AWS S3, MinIO, etc.).
type s3Storage struct {
	client *minio.Client
}

// New constructs a Storage backed by an S3-compatible object store. endpoint is
// host:port (no scheme); set secure to true to use TLS. It takes plain
// primitives rather than a config type so the storage package stays decoupled
// from application config.
func New(endpoint, accessKeyID, secretAccessKey string, secure bool) (Storage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, err
	}
	return &s3Storage{client: client}, nil
}

func (s *s3Storage) Get(ctx context.Context, bucket, key string) (*Object, error) {
	return nil, errNotImplemented
}

func (s *s3Storage) Put(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType string) error {
	return errNotImplemented
}

func (s *s3Storage) List(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error) {
	return nil, errNotImplemented
}

func (s *s3Storage) Stat(ctx context.Context, bucket, key string) (ObjectInfo, error) {
	return ObjectInfo{}, errNotImplemented
}
