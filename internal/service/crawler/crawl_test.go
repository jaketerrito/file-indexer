package crawler

import (
	"context"
	"errors"
	"testing"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/stretchr/testify/mock"
)

// walkOver returns a RunAndReturn implementation that invokes the supplied
// callback once per ref, mimicking a real ObjectStore.Walk. It returns the
// first callback error, matching the production loop's short-circuit behavior.
func walkOver(refs ...*pb.FileRef) func(ctx context.Context, fn func(*pb.FileRef) error) error {
	return func(ctx context.Context, fn func(*pb.FileRef) error) error {
		for _, ref := range refs {
			if err := fn(ref); err != nil {
				return err
			}
		}
		return nil
	}
}

func TestRun(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(&pb.FileRef{Key: "a"}, &pb.FileRef{Key: "b"}))

	client := NewMockIndexer(t)
	client.EXPECT().Index(mock.Anything, mock.MatchedBy(func(req *pb.IndexRequest) bool {
		return req.GetRef().GetKey() == "a"
	})).Return(&pb.IndexResponse{Status: "OK"}, nil)
	client.EXPECT().Index(mock.Anything, mock.MatchedBy(func(req *pb.IndexRequest) bool {
		return req.GetRef().GetKey() == "b"
	})).Return(&pb.IndexResponse{Status: "OK"}, nil)

	c := New(store, client)
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
}

func TestRunIndexError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).
		RunAndReturn(walkOver(&pb.FileRef{Key: "a"}))

	client := NewMockIndexer(t)
	client.EXPECT().Index(mock.Anything, mock.Anything).
		Return(nil, errors.New("index failed"))

	c := New(store, client)
	if err := c.Run(); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunWalkError(t *testing.T) {
	store := NewMockObjectStore(t)
	store.EXPECT().Walk(mock.Anything, mock.Anything).Return(errors.New("walk failed"))

	// Index must never be called when Walk itself fails.
	client := NewMockIndexer(t)

	c := New(store, client)
	if err := c.Run(); err == nil {
		t.Fatal("expected error")
	}
	client.AssertNotCalled(t, "Index", mock.Anything, mock.Anything)
}
