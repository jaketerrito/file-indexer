//go:build integration

package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Integration tests require a running S3-compatible store, configured via the
// same S3_* environment variables as config.Load. The -tags=integration build
// tag is the opt-in; once opted in, a missing store is a test failure, not a
// skip. Run them with:
//
//	just test-integration
//
// or against the Tilt dev environment manually:
//
//	export S3_ENDPOINT=localhost:9000 S3_ACCESS_ID=user S3_SECRET=password
//	go test -tags=integration -race ./internal/storage/
type s3Env struct {
	endpoint string
	accessID string
	secret   string
	region   string
}

func testEnv(t *testing.T) s3Env {
	t.Helper()
	if os.Getenv("S3_ENDPOINT") == "" {
		t.Fatal("integration tests require S3_ENDPOINT to be set (see just test-integration)")
	}
	return s3Env{
		endpoint: os.Getenv("S3_ENDPOINT"),
		accessID: os.Getenv("S3_ACCESS_ID"),
		secret:   os.Getenv("S3_SECRET"),
		region:   os.Getenv("S3_REGION"),
	}
}

// setupBucket creates a bucket unique to this test, registers cleanup that
// empties and removes it, and returns a Storage bound to it together with a
// raw minio client for seeding objects.
func setupBucket(t *testing.T) (Storage, *minio.Client, string) {
	t.Helper()
	env := testEnv(t)
	ctx := context.Background()

	client, err := minio.New(env.endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(env.accessID, env.secret, ""),
		Secure: false,
	})
	if err != nil {
		t.Fatalf("minio.New: %v", err)
	}

	bucket := fmt.Sprintf("it-%d", time.Now().UnixNano())
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("MakeBucket: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for obj := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if obj.Err != nil {
				t.Errorf("cleanup list objects: %v", obj.Err)
				continue
			}
			if err := client.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
				t.Errorf("cleanup remove object %q: %v", obj.Key, err)
			}
		}
		if err := client.RemoveBucket(ctx, bucket); err != nil {
			t.Errorf("cleanup remove bucket: %v", err)
		}
	})

	s, err := New(env.endpoint, env.accessID, env.secret, false, bucket, env.region)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, client, bucket
}

func putObject(t *testing.T, client *minio.Client, bucket, key, content, contentType string) {
	t.Helper()
	_, err := client.PutObject(context.Background(), bucket, key,
		strings.NewReader(content), int64(len(content)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		t.Fatalf("PutObject(%q): %v", key, err)
	}
}

func TestNewInvalidEndpoint(t *testing.T) {
	testEnv(t)

	if _, err := New("localhost:9000/not-just-a-host", "id", "secret", false, "bucket", "us-east-1"); err == nil {
		t.Fatal("New with invalid endpoint: want error, got nil")
	}
}

func TestStat(t *testing.T) {
	s, client, bucket := setupBucket(t)
	ctx := context.Background()

	putObject(t, client, bucket, "stat.txt", "hello stat", "text/plain")

	info, err := s.Stat(ctx, "stat.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Key != "stat.txt" {
		t.Errorf("Key = %q, want %q", info.Key, "stat.txt")
	}
	if info.Size != int64(len("hello stat")) {
		t.Errorf("Size = %d, want %d", info.Size, len("hello stat"))
	}
	if info.ContentType != "text/plain" {
		t.Errorf("ContentType = %q, want %q", info.ContentType, "text/plain")
	}
	if time.Since(info.LastModified) > time.Minute {
		t.Errorf("LastModified = %v, want recent", info.LastModified)
	}

	if _, err := s.Stat(ctx, "no-such-key"); err == nil {
		t.Error("Stat(no-such-key): want error, got nil")
	}
}

func TestGet(t *testing.T) {
	s, client, bucket := setupBucket(t)
	ctx := context.Background()

	putObject(t, client, bucket, "get.txt", "hello get", "text/plain")

	obj, err := s.Get(ctx, "get.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() {
		if err := obj.Reader.Close(); err != nil {
			t.Errorf("close reader: %v", err)
		}
	}()

	if obj.Key != "get.txt" {
		t.Errorf("Key = %q, want %q", obj.Key, "get.txt")
	}
	if obj.Size != int64(len("hello get")) {
		t.Errorf("Size = %d, want %d", obj.Size, len("hello get"))
	}
	content, err := io.ReadAll(obj.Reader)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	if string(content) != "hello get" {
		t.Errorf("content = %q, want %q", content, "hello get")
	}

	// Missing keys surface as an error from the stat inside Get.
	if _, err := s.Get(ctx, "no-such-key"); err == nil {
		t.Error("Get(no-such-key): want error, got nil")
	}
}

func TestGetURL(t *testing.T) {
	s, client, bucket := setupBucket(t)
	ctx := context.Background()

	putObject(t, client, bucket, "url.txt", "hello url", "text/plain")

	rawURL, err := s.GetURL(ctx, "url.txt")
	if err != nil {
		t.Fatalf("GetURL: %v", err)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse presigned URL %q: %v", rawURL, err)
	}
	if got := u.Query().Get("response-content-disposition"); got != "attachment" {
		t.Errorf("response-content-disposition = %q, want %q", got, "attachment")
	}

	// The presigned URL must actually serve the object.
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatalf("GET presigned URL: %v", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET presigned URL status = %d, want 200", resp.StatusCode)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if string(content) != "hello url" {
		t.Errorf("content = %q, want %q", content, "hello url")
	}
}

func TestWalk(t *testing.T) {
	s, client, bucket := setupBucket(t)
	ctx := context.Background()

	want := map[string]bool{"walk-1.txt": true, "walk-2.txt": true, "walk-3.txt": true}
	for key := range want {
		putObject(t, client, bucket, key, "x", "text/plain")
	}

	got := map[string]bool{}
	err := s.Walk(ctx, func(ref *pb.FileRef) error {
		got[ref.GetKey()] = true
		return nil
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for key := range want {
		if !got[key] {
			t.Errorf("Walk did not visit %q (visited: %v)", key, got)
		}
	}

	// Callback errors must propagate.
	wantErr := fmt.Errorf("callback failure")
	err = s.Walk(ctx, func(*pb.FileRef) error { return wantErr })
	if err == nil {
		t.Fatal("Walk with failing callback: want error, got nil")
	}
}

func TestDelete(t *testing.T) {
	s, client, bucket := setupBucket(t)
	ctx := context.Background()

	putObject(t, client, bucket, "delete.txt", "bye", "text/plain")

	if err := s.Delete(ctx, "delete.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Stat(ctx, "delete.txt"); err == nil {
		t.Error("Stat after Delete: want error, got nil")
	}

	// Deleting a missing key is a no-op per S3 semantics.
	if err := s.Delete(ctx, "no-such-key"); err != nil {
		t.Errorf("Delete(no-such-key) = %v, want nil", err)
	}
}
