package storage

import (
	"context"
	"log/slog"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Storage is the S3-backed implementation of Storage. It uses the minio-go
// client, which speaks the S3 API and works against any S3-compatible store
// (AWS S3, MinIO, etc.).
type s3Storage struct {
	client *minio.Client
	// presignClient signs the URLs returned by GetURL. It equals client
	// unless a public endpoint was configured (see NewWithPublicEndpoint).
	presignClient *minio.Client
	bucket        string
}

// New constructs a Storage backed by an S3-compatible object store. endpoint is
// host:port (no scheme); set secure to true to use TLS. bucket is the single
// bucket this Storage operates on. region is used for SigV4 signing; when
// empty, the client discovers the bucket's region via a location lookup. It
// takes plain primitives rather than a config type so the storage package
// stays decoupled from application config.
func New(endpoint, accessKeyID, secretAccessKey string, secure bool, bucket, region string) (Storage, error) {
	return NewWithPublicEndpoint(endpoint, "", accessKeyID, secretAccessKey, secure, bucket, region)
}

// NewWithPublicEndpoint is New for services that hand presigned URLs to
// clients outside the cluster network (e.g. browsers). A presigned URL's
// signature binds the host, so URLs signed against an in-cluster endpoint
// (like "local-s3:9000") are useless to external clients. When publicEndpoint
// (host:port, no scheme) is non-empty, GetURL signs against it instead; all
// other operations keep using endpoint. Pass "" to sign against endpoint.
//
// region is pinned on both clients rather than discovered via a bucket
// location lookup: publicEndpoint is generally not reachable from where this
// service runs, and pinning also spares the primary client a network round
// trip at startup. Callers using a public endpoint must therefore supply a
// non-empty region, or presigning will attempt a location lookup against
// publicEndpoint.
func NewWithPublicEndpoint(endpoint, publicEndpoint, accessKeyID, secretAccessKey string, secure bool, bucket, region string) (Storage, error) {
	creds := credentials.NewStaticV4(accessKeyID, secretAccessKey, "")
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  creds,
		Secure: secure,
		Region: region,
	})
	if err != nil {
		return nil, err
	}

	presignClient := client
	if publicEndpoint != "" {
		presignClient, err = minio.New(publicEndpoint, &minio.Options{
			Creds:  creds,
			Secure: secure,
			Region: region,
		})
		if err != nil {
			return nil, err
		}
	}

	return &s3Storage{client: client, presignClient: presignClient, bucket: bucket}, nil
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

func (s *s3Storage) GetURL(ctx context.Context, key string) (string, error) {
	// Set request parameters
	reqParams := make(url.Values)
	reqParams.Set("response-content-disposition", "attachment")

	expires := time.Duration(1000) * time.Second

	// Gernerate presigned get object url.
	presignedURL, err := s.presignClient.PresignedGetObject(ctx, s.bucket, key, expires, reqParams)
	if err != nil {
		return "", err
	}
	return presignedURL.String(), nil
}

func (s *s3Storage) Walk(ctx context.Context, fn func(ObjectInfo) error) error {
	// Recursive listing: without it S3 returns common prefixes ("dir/")
	// instead of the objects beneath them.
	for obj := range s.client.ListObjectsIter(ctx, s.bucket, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err != nil {
			return obj.Err
		}
		// Listings do not carry ContentType; ObjectInfo.ContentType stays
		// empty and consumers needing it must Stat the key.
		if err := fn(ObjectInfo{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified,
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

func (s *s3Storage) Delete(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	return err
}
