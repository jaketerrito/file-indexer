//go:build integration

package storage

import (
	"os"
	"testing"
)

// Integration tests require a running S3-compatible store and database.
// Run them with:  go test -tags=integration -race ./...
//
// Example:
//
//	export S3_ENDPOINT=localhost:9000
//	export S3_ACCESS_ID=minioadmin
//	export S3_SECRET=minioadmin
//	export S3_BUCKET=test
//	go test -tags=integration -v -race ./internal/storage/
func TestNotSkipped(t *testing.T) {
	if os.Getenv("S3_ENDPOINT") == "" {
		t.Skip("S3_ENDPOINT not set; skipping integration test")
	}
}
