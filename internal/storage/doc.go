// Package storage provides a domain-focused wrapper around an object store
// (MinIO / S3). Callers depend on the Storage interface rather than on the
// concrete minio-go client, which keeps domain code decoupled and testable.
package storage
