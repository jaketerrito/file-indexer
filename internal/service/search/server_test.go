package search

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	"net"
	"testing"
	"time"

	pb "file-indexer/internal/pb/service/v1"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func testFile(id int64, key string) db.File {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return db.File{
		ID: id, Key: key,
		ContentType: pgtype.Text{String: "text/plain", Valid: true},
		SizeBytes:   pgtype.Int8{Int64: id * 10, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
	}
}

func TestNew(t *testing.T) {
	queries := NewMockFileIndex(t)

	srv := New(":1234", queries)

	if srv == nil {
		t.Fatal("New returned nil")
	}
	if srv.addr != ":1234" {
		t.Errorf("addr = %q, want %q", srv.addr, ":1234")
	}
	if srv.queries != queries {
		t.Error("New did not wire dependencies")
	}
}

func TestListFilesDefaults(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern:         "%",
		ContentTypePattern: "",
		HasCursor:          false,
		PageLimit:          defaultPageSize + 1,
	}).Return([]db.File{testFile(1, "a"), testFile(2, "b")}, nil)

	srv := SearchServer{queries: queries}

	resp, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetFiles()) != 2 {
		t.Fatalf("got %d files, want 2", len(resp.GetFiles()))
	}
	if resp.GetFiles()[0].GetKey() != "a" || resp.GetFiles()[1].GetKey() != "b" {
		t.Errorf("files = %+v", resp.GetFiles())
	}
	if resp.GetNextPageToken() != "" {
		t.Errorf("next_page_token = %q, want empty", resp.GetNextPageToken())
	}
}

func TestListFilesPageSizeClamped(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern: "%",
		PageLimit:  maxPageSize + 1,
	}).Return(nil, nil)

	srv := SearchServer{queries: queries}

	if _, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{PageSize: 10_000}); err != nil {
		t.Fatal(err)
	}
}

func TestListFilesNegativePageSize(t *testing.T) {
	srv := SearchServer{queries: NewMockFileIndex(t)}

	_, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{PageSize: -1})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestListFilesPrefixEscaped(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern: `docs\%1\_a\\%`,
		PageLimit:  defaultPageSize + 1,
	}).Return(nil, nil)

	srv := SearchServer{queries: queries}

	if _, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{Prefix: `docs%1_a\`}); err != nil {
		t.Fatal(err)
	}
}

func TestListFilesContentTypeExact(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern:         "%",
		ContentTypePattern: "image/png",
		PageLimit:          defaultPageSize + 1,
	}).Return(nil, nil)

	srv := SearchServer{queries: queries}

	if _, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{ContentType: "image/png"}); err != nil {
		t.Fatal(err)
	}
}

func TestListFilesContentTypeCategory(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern:         "%",
		ContentTypePattern: "image/%",
		PageLimit:          defaultPageSize + 1,
	}).Return(nil, nil)

	srv := SearchServer{queries: queries}

	if _, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{ContentType: "image/"}); err != nil {
		t.Fatal(err)
	}
}

func TestListFilesSortDispatch(t *testing.T) {
	// Each supported (field, order) combination must dispatch to its own
	// query; the mocks fail the test on any unexpected call.
	tests := []struct {
		name  string
		field pb.SortField
		order pb.SortOrder
		setup func(*MockFileIndex)
	}{
		{"key asc", pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_ASC, func(m *MockFileIndex) {
			m.EXPECT().ListFilesByKeyAsc(mock.Anything, mock.Anything).Return(nil, nil)
		}},
		{"key desc", pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_DESC, func(m *MockFileIndex) {
			m.EXPECT().ListFilesByKeyDesc(mock.Anything, mock.Anything).Return(nil, nil)
		}},
		{"created_at asc", pb.SortField_SORT_FIELD_CREATED_AT, pb.SortOrder_SORT_ORDER_ASC, func(m *MockFileIndex) {
			m.EXPECT().ListFilesByCreatedAtAsc(mock.Anything, mock.Anything).Return(nil, nil)
		}},
		{"created_at desc", pb.SortField_SORT_FIELD_CREATED_AT, pb.SortOrder_SORT_ORDER_DESC, func(m *MockFileIndex) {
			m.EXPECT().ListFilesByCreatedAtDesc(mock.Anything, mock.Anything).Return(nil, nil)
		}},
		{"size asc", pb.SortField_SORT_FIELD_SIZE, pb.SortOrder_SORT_ORDER_ASC, func(m *MockFileIndex) {
			m.EXPECT().ListFilesBySizeAsc(mock.Anything, mock.Anything).Return(nil, nil)
		}},
		{"size desc", pb.SortField_SORT_FIELD_SIZE, pb.SortOrder_SORT_ORDER_DESC, func(m *MockFileIndex) {
			m.EXPECT().ListFilesBySizeDesc(mock.Anything, mock.Anything).Return(nil, nil)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := NewMockFileIndex(t)
			tt.setup(queries)

			srv := SearchServer{queries: queries}

			_, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{
				SortField: tt.field,
				SortOrder: tt.order,
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestListFilesUnknownSortField(t *testing.T) {
	srv := SearchServer{queries: NewMockFileIndex(t)}

	_, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{SortField: 99})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestListFilesUnknownSortOrder(t *testing.T) {
	srv := SearchServer{queries: NewMockFileIndex(t)}

	_, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{SortOrder: 99})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestListFilesPagination(t *testing.T) {
	// First page: 3 rows for a limit of 2 means another page exists; the
	// third row must be dropped and the token must point at the second.
	files := []db.File{testFile(1, "a"), testFile(2, "b"), testFile(3, "c")}

	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern: "%",
		PageLimit:  3,
	}).Return(files, nil)

	srv := SearchServer{queries: queries}

	resp, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetFiles()) != 2 {
		t.Fatalf("got %d files, want 2", len(resp.GetFiles()))
	}
	if resp.GetNextPageToken() == "" {
		t.Fatal("next_page_token is empty, want cursor")
	}

	cur, err := decodeCursor(resp.GetNextPageToken())
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if cur.LastID != 2 || cur.Key != "b" {
		t.Errorf("cursor = %+v, want LastID=2 Key=b", cur)
	}

	// Second page: the cursor must be passed through to the query.
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern: "%",
		HasCursor:  true,
		LastKey:    "b",
		LastID:     2,
		PageLimit:  3,
	}).Return(files[2:], nil)

	resp2, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{
		PageSize:  2,
		PageToken: resp.GetNextPageToken(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp2.GetFiles()) != 1 || resp2.GetFiles()[0].GetKey() != "c" {
		t.Errorf("files = %+v, want single file c", resp2.GetFiles())
	}
	if resp2.GetNextPageToken() != "" {
		t.Errorf("next_page_token = %q, want empty on last page", resp2.GetNextPageToken())
	}
}

func TestListFilesInvalidPageToken(t *testing.T) {
	srv := SearchServer{queries: NewMockFileIndex(t)}

	_, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{PageToken: "!!!not-a-token"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestListFilesPageTokenSortMismatch(t *testing.T) {
	token := encodeCursor(newCursor(pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_ASC, testFile(1, "a")))

	srv := SearchServer{queries: NewMockFileIndex(t)}

	_, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{
		PageToken: token,
		SortField: pb.SortField_SORT_FIELD_SIZE,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestListFilesQueryError(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, mock.Anything).Return(nil, errors.New("db error"))

	srv := SearchServer{queries: queries}

	if _, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestDbFileToProto(t *testing.T) {
	now := time.Now()
	f := db.File{
		ID: 1, Key: "k",
		ContentType: pgtype.Text{String: "image/png", Valid: true},
		SizeBytes:   pgtype.Int8{Int64: 200, Valid: true},
		CreatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
		UpdatedAt:   pgtype.Timestamptz{Time: now, Valid: true},
	}
	pf := dbFileToProto(f)
	if pf.Id != 1 || pf.Key != "k" || pf.ContentType != "image/png" || pf.SizeBytes != 200 {
		t.Errorf("dbFileToProto = %+v", pf)
	}
	if !pf.CreatedAt.AsTime().Equal(now) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", pf.CreatedAt.AsTime(), now)
	}
	if !pf.UpdatedAt.AsTime().Equal(now) {
		t.Errorf("UpdatedAt mismatch")
	}
}

func TestDbFileToProtoNullFields(t *testing.T) {
	f := db.File{
		ID: 2, Key: "nulls",
	}
	pf := dbFileToProto(f)
	if pf.Id != 2 || pf.ContentType != "" || pf.SizeBytes != 0 {
		t.Errorf("dbFileToProto = %+v", pf)
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
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, mock.Anything).
		Return([]db.File{testFile(7, "obj-key")}, nil)

	addr := freeAddr(t)
	srv := New(addr, queries)

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

	client := pb.NewSearchServiceClient(conn)
	resp, err := client.ListFiles(ctx, &pb.ListFilesRequest{}, grpc.WaitForReady(true))
	if err != nil {
		t.Fatalf("ListFiles over gRPC: %v", err)
	}
	if len(resp.GetFiles()) != 1 || resp.GetFiles()[0].GetKey() != "obj-key" {
		t.Errorf("ListFiles = %+v, want single file obj-key", resp.GetFiles())
	}

	select {
	case err := <-errCh:
		t.Fatalf("Serve exited unexpectedly: %v", err)
	default:
	}
}

func TestServeBadAddr(t *testing.T) {
	srv := New("256.256.256.256:0", NewMockFileIndex(t))
	if err := srv.Serve(); err == nil {
		t.Fatal("Serve with bad addr: want error, got nil")
	}
}
