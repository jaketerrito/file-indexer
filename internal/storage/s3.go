package storage

import (
	"context"
	"errors"
	"file-indexer/internal/pb"
	"io"
	"log/slog"

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
	bucket string
}

// New constructs a Storage backed by an S3-compatible object store. endpoint is
// host:port (no scheme); set secure to true to use TLS. bucket is the single
// bucket this Storage operates on. It takes plain primitives rather than a
// config type so the storage package stays decoupled from application config.
func New(endpoint, accessKeyID, secretAccessKey string, secure bool, bucket string) (Storage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, err
	}
	return &s3Storage{client: client, bucket: bucket}, nil
}

func (s *s3Storage) Get(ctx context.Context, key string) (*Object, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}

	info, err := obj.Stat()
	if err != nil {
		if closeErr := obj.Close(); closeErr != nil {
			slog.Warn("close object after stat failure", "key", key, "statErr", err, "closeErr", closeErr)
		}
		return nil, err
	}

	return &Object{
		ObjectInfo: ObjectInfo{
			Key:          info.Key,
			Size:         info.Size,
			ContentType:  info.ContentType,
			LastModified: info.LastModified,
		},
		Reader: obj,
	}, nil
}

func (s *s3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	return errNotImplemented
}

func (s *s3Storage) Walk(ctx context.Context, fn func(*pb.FileRef) error) error {
	for obj := range s.client.ListObjectsIter(ctx, s.bucket, minio.ListObjectsOptions{}) {
		if obj.Err != nil {
			return obj.Err
		}
		if err := fn(&pb.FileRef{
			Key: obj.Key,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *s3Storage) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}

	return ObjectInfo{Key: info.Key, Size: info.Size, ContentType: info.ContentType, LastModified: info.LastModified}, nil
}
