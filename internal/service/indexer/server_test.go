package indexer

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"file-indexer/internal/storage"
	"net"
	"testing"
	"time"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestNew(t *testing.T) {
	store := NewMockObjectStore(t)
	queries := NewMockFileIndex(t)

	srv := New(":1234", store, queries)

	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.addr != ":1234" {
		t.Errorf("addr = %q, want %q", srv.addr, ":1234")
	}
	if srv.storage != store || srv.queries != queries {
		t.Error("New did not wire dependencies")
	}
}

func TestIndex(t *testing.T) {
	now := time.Now()
	info := storage.ObjectInfo{
		Key:          "obj-key",
		Size:         100,
		ContentType:  "text/plain",
		LastModified: now,
	}

	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "obj-key").Return(info, nil)

	queries := NewMockFileIndex(t)
	queries.EXPECT().
		UpsertFile(mock.Anything, mock.MatchedBy(func(arg db.UpsertFileParams) bool {
			return arg.Key == "obj-key" &&
				arg.ContentType.String == "text/plain" && arg.ContentType.Valid &&
				arg.SizeBytes.Int64 == 100 && arg.SizeBytes.Valid &&
				arg.CreatedAt.Time.Equal(now) && arg.CreatedAt.Valid &&
				arg.UpdatedAt.Time.Equal(now) && arg.UpdatedAt.Valid
		})).
		Return(db.File{ID: 1, Key: "obj-key"}, nil)

	srv := IndexerServer{storage: store, queries: queries}

	resp, err := srv.Index(context.Background(), &pb.IndexRequest{
		Ref: &pb.FileRef{Key: "obj-key"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != "OK" {
		t.Errorf("Status = %q, want %q", resp.Status, "OK")
	}
}

func TestIndexStatError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "missing").Return(storage.ObjectInfo{}, errors.New("not found"))

	queries := NewMockFileIndex(t)
	// UpsertFile must never be called when Stat fails.

	srv := IndexerServer{storage: store, queries: queries}

	_, err := srv.Index(context.Background(), &pb.IndexRequest{
		Ref: &pb.FileRef{Key: "missing"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	queries.AssertNotCalled(t, "UpsertFile", mock.Anything, mock.Anything)
}

func TestIndexUpsertFileError(t *testing.T) {
	info := storage.ObjectInfo{Key: "obj-key", Size: 1, ContentType: "text/plain", LastModified: time.Now()}

	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "obj-key").Return(info, nil)

	queries := NewMockFileIndex(t)
	queries.EXPECT().UpsertFile(mock.Anything, mock.Anything).Return(db.File{}, errors.New("db error"))

	srv := IndexerServer{storage: store, queries: queries}

	_, err := srv.Index(context.Background(), &pb.IndexRequest{
		Ref: &pb.FileRef{Key: "obj-key"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// freeAddr reserves an ephemeral port and returns its address. There is a
// small window between closing the probe listener and Serve re-binding it,
// which is acceptable for tests.
func freeAddr(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := lis.Addr().String()
	if err := lis.Close(); err != nil {
		t.Fatalf("release port: %v", err)
	}
	return addr
}

func TestServe(t *testing.T) {
	info := storage.ObjectInfo{Key: "obj-key", Size: 1, ContentType: "text/plain", LastModified: time.Now()}

	store := NewMockObjectStore(t)
	store.EXPECT().Stat(mock.Anything, "obj-key").Return(info, nil)

	queries := NewMockFileIndex(t)
	queries.EXPECT().UpsertFile(mock.Anything, mock.Anything).Return(db.File{ID: 1, Key: "obj-key"}, nil)

	addr := freeAddr(t)
	srv := New(addr, store, queries)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve() }()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client := pb.NewIndexerServiceClient(conn)
	resp, err := client.Index(ctx, &pb.IndexRequest{Ref: &pb.FileRef{Key: "obj-key"}},
		grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("Index over gRPC: %v", err)
	}
	if resp.GetStatus() != "OK" {
		t.Errorf("Status = %q, want %q", resp.GetStatus(), "OK")
	}

	select {
	case err := <-errCh:
		t.Fatalf("Serve exited unexpectedly: %v", err)
	default:
	}
}

func TestServeBadAddr(t *testing.T) {
	srv := New("256.256.256.256:0", NewMockObjectStore(t), NewMockFileIndex(t))
	if err := srv.Serve(); err == nil {
		t.Fatal("Serve with bad addr: want error, got nil")
	}
}
