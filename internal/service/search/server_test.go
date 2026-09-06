package search

import (
	"context"
	"errors"
	"file-indexer/internal/db"
	cursorv1 "file-indexer/internal/pb/cursor/v1"
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

func testFile(id int64, key string) db.FileInfo {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return db.FileInfo{
		ID: id, Key: key,
		ContentType:  pgtype.Text{String: "text/plain", Valid: true},
		SizeBytes:    pgtype.Int8{Int64: id * 10, Valid: true},
		CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		LastModified: pgtype.Timestamptz{Time: now, Valid: true},
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
	}).Return([]db.FileInfo{testFile(1, "a"), testFile(2, "b")}, nil)
	queries.EXPECT().GetIndexQueueStatuses(mock.Anything, mock.Anything).Return(nil, nil)

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
		{"last_modified asc", pb.SortField_SORT_FIELD_LAST_MODIFIED, pb.SortOrder_SORT_ORDER_ASC, func(m *MockFileIndex) {
			m.EXPECT().ListFilesByLastModifiedAsc(mock.Anything, mock.Anything).Return(nil, nil)
		}},
		{"last_modified desc", pb.SortField_SORT_FIELD_LAST_MODIFIED, pb.SortOrder_SORT_ORDER_DESC, func(m *MockFileIndex) {
			m.EXPECT().ListFilesByLastModifiedDesc(mock.Anything, mock.Anything).Return(nil, nil)
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
	files := []db.FileInfo{testFile(1, "a"), testFile(2, "b"), testFile(3, "c")}

	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern: "%",
		PageLimit:  3,
	}).Return(files, nil)
	queries.EXPECT().GetIndexQueueStatuses(mock.Anything, mock.Anything).Return(nil, nil)

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
	if cur.GetLastId() != 2 || cur.GetKey() != "b" {
		t.Errorf("cursor = %+v, want LastId=2 Key=b", cur)
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

func TestListFilesPageTokenQueryMismatch(t *testing.T) {
	// AIP-158: all arguments other than page_size must match the call that
	// produced the token.
	token := encodeCursor(newCursor(pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_ASC, "docs/", "image/", testFile(1, "a")))

	tests := []struct {
		name string
		req  *pb.ListFilesRequest
	}{
		{"sort_field changed", &pb.ListFilesRequest{
			PageToken: token, SortField: pb.SortField_SORT_FIELD_SIZE, Prefix: "docs/", ContentType: "image/",
		}},
		{"sort_order changed", &pb.ListFilesRequest{
			PageToken: token, SortOrder: pb.SortOrder_SORT_ORDER_DESC, Prefix: "docs/", ContentType: "image/",
		}},
		{"prefix changed", &pb.ListFilesRequest{
			PageToken: token, Prefix: "other/", ContentType: "image/",
		}},
		{"content_type changed", &pb.ListFilesRequest{
			PageToken: token, Prefix: "docs/", ContentType: "video/",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := SearchServer{queries: NewMockFileIndex(t)}

			_, err := srv.ListFiles(context.Background(), tt.req)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("error = %v, want InvalidArgument", err)
			}
		})
	}
}

func TestListFilesPageTokenCarriesFilters(t *testing.T) {
	// A token minted with filters is accepted when the request repeats them,
	// and the filters keep applying to the next page.
	token := encodeCursor(newCursor(pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_ASC, "docs/", "image/", testFile(1, "docs/a")))

	queries := NewMockFileIndex(t)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern:         "docs/%",
		ContentTypePattern: "image/%",
		HasCursor:          true,
		LastKey:            "docs/a",
		LastID:             1,
		PageLimit:          defaultPageSize + 1,
	}).Return(nil, nil)

	srv := SearchServer{queries: queries}

	_, err := srv.ListFiles(context.Background(), &pb.ListFilesRequest{
		PageToken:   token,
		Prefix:      "docs/",
		ContentType: "image/",
	})
	if err != nil {
		t.Fatal(err)
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
	f := db.FileInfo{
		ID: 1, Key: "k",
		ContentType:  pgtype.Text{String: "image/png", Valid: true},
		SizeBytes:    pgtype.Int8{Int64: 200, Valid: true},
		CreatedAt:    pgtype.Timestamptz{Time: now, Valid: true},
		LastModified: pgtype.Timestamptz{Time: now, Valid: true},
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
	f := db.FileInfo{
		ID: 2, Key: "nulls",
	}
	pf := dbFileToProto(f)
	if pf.Id != 2 || pf.ContentType != "" || pf.SizeBytes != 0 {
		t.Errorf("dbFileToProto = %+v", pf)
	}
	if pf.CreatedAt != nil || pf.UpdatedAt != nil {
		t.Errorf("timestamps = (%v, %v), want unset for NULLs", pf.CreatedAt, pf.UpdatedAt)
	}
}

func TestPreviewStatusMapping(t *testing.T) {
	image := db.FileInfo{ContentType: pgtype.Text{String: "image/png", Valid: true}}
	txt := db.FileInfo{ContentType: pgtype.Text{String: "text/plain", Valid: true}}
	unknown := db.FileInfo{}

	tests := []struct {
		name    string
		f       db.FileInfo
		qStatus string
		qQueued bool
		want    pb.PreviewStatus
	}{
		{"ready with key", db.FileInfo{PreviewKey: pgtype.Text{String: "pk", Valid: true}}, "pending", true, pb.PreviewStatus_PREVIEW_STATUS_READY},
		{"pending", image, "pending", true, pb.PreviewStatus_PREVIEW_STATUS_PENDING},
		{"processing", image, "claimed", true, pb.PreviewStatus_PREVIEW_STATUS_PROCESSING},
		{"failed", image, "error", true, pb.PreviewStatus_PREVIEW_STATUS_FAILED},
		{"done none", image, "done", true, pb.PreviewStatus_PREVIEW_STATUS_NONE},
		{"no row image", image, "", false, pb.PreviewStatus_PREVIEW_STATUS_PENDING},
		{"no row text", txt, "", false, pb.PreviewStatus_PREVIEW_STATUS_NONE},
		{"no row unknown", unknown, "", false, pb.PreviewStatus_PREVIEW_STATUS_PENDING},
		{"ready ignores done", db.FileInfo{PreviewKey: pgtype.Text{String: "pk", Valid: true}}, "done", true, pb.PreviewStatus_PREVIEW_STATUS_READY},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := previewStatus(tt.f, tt.qStatus, tt.qQueued)
			if got != tt.want {
				t.Errorf("previewStatus = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestListDirectoryRoot(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListChildDirectories(mock.Anything, db.ListChildDirectoriesParams{
		Parent:    "",
		PageLimit: defaultPageSize + 1,
	}).Return([]string{"docs/", "other/"}, nil)
	queries.EXPECT().GetIndexQueueStatuses(mock.Anything, mock.Anything).Return(nil, nil)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern: "%",
		DirectOnly: true,
		DirPrefix:  "",
		PageLimit:  defaultPageSize - 2 + 1,
	}).Return([]db.FileInfo{testFile(1, "root.txt")}, nil)

	srv := SearchServer{queries: queries}

	resp, err := srv.ListDirectory(context.Background(), &pb.ListDirectoryRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetDirectories()) != 2 || resp.GetDirectories()[0] != "docs/" || resp.GetDirectories()[1] != "other/" {
		t.Errorf("directories = %v, want [docs/ other/]", resp.GetDirectories())
	}
	if len(resp.GetFiles()) != 1 || resp.GetFiles()[0].GetKey() != "root.txt" {
		t.Errorf("files = %+v, want [root.txt]", resp.GetFiles())
	}
	if resp.GetNextPageToken() != "" {
		t.Errorf("next_page_token = %q, want empty", resp.GetNextPageToken())
	}
}

func TestListDirectoryDirectoriesPageTruncated(t *testing.T) {
	// More directories exist than fit on the page: the files phase must not
	// run at all, and the token must resume the directories phase.
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListChildDirectories(mock.Anything, db.ListChildDirectoriesParams{
		Parent:    "docs/",
		PageLimit: 3,
	}).Return([]string{"docs/a/", "docs/b/", "docs/c/"}, nil)

	srv := SearchServer{queries: queries}

	resp, err := srv.ListDirectory(context.Background(), &pb.ListDirectoryRequest{Path: "docs/", PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetDirectories()) != 2 || resp.GetDirectories()[1] != "docs/b/" {
		t.Errorf("directories = %v, want [docs/a/ docs/b/]", resp.GetDirectories())
	}
	if len(resp.GetFiles()) != 0 {
		t.Errorf("files = %+v, want none (directories phase not exhausted)", resp.GetFiles())
	}
	if resp.GetNextPageToken() == "" {
		t.Fatal("next_page_token is empty, want cursor")
	}

	cur, err := decodeCursor(resp.GetNextPageToken())
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if cur.GetPhase() != cursorv1.ListPhase_LIST_PHASE_DIRECTORIES || cur.GetLastDir() != "docs/b/" {
		t.Errorf("cursor = %+v, want phase=DIRECTORIES lastDir=docs/b/", cur)
	}
}

func TestListDirectoryResumeDirectoriesPhase(t *testing.T) {
	// The directories table is an exact keyset index: resuming needs only
	// the previous page's last path as `after`, no skip trick.
	token := encodeCursor(newDirCursor(pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_ASC, "docs/", "docs/b/"))

	queries := NewMockFileIndex(t)
	queries.EXPECT().ListChildDirectories(mock.Anything, db.ListChildDirectoriesParams{
		Parent:    "docs/",
		HasCursor: true,
		After:     "docs/b/",
		PageLimit: 3,
	}).Return([]string{"docs/c/"}, nil)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern: "docs/%",
		DirectOnly: true,
		DirPrefix:  "docs/",
		PageLimit:  2,
	}).Return(nil, nil)

	srv := SearchServer{queries: queries}

	resp, err := srv.ListDirectory(context.Background(), &pb.ListDirectoryRequest{
		Path: "docs/", PageSize: 2, PageToken: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetDirectories()) != 1 || resp.GetDirectories()[0] != "docs/c/" {
		t.Errorf("directories = %v, want [docs/c/]", resp.GetDirectories())
	}
}

func TestListDirectoryDirectoriesFillPageExactlyStillProbesFiles(t *testing.T) {
	// Directories exhausted (fetched == limit, not limit+1) leaves remaining
	// == 0, but a FILES-phase token must still be emitted if a file exists,
	// so the next call doesn't silently skip it.
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListChildDirectories(mock.Anything, db.ListChildDirectoriesParams{
		Parent:    "",
		PageLimit: 3,
	}).Return([]string{"a/", "b/"}, nil)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern: "%",
		DirectOnly: true,
		DirPrefix:  "",
		PageLimit:  1,
	}).Return([]db.FileInfo{testFile(9, "z.txt")}, nil)

	srv := SearchServer{queries: queries}

	resp, err := srv.ListDirectory(context.Background(), &pb.ListDirectoryRequest{PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetFiles()) != 0 {
		t.Errorf("files = %+v, want none (remaining budget was 0)", resp.GetFiles())
	}
	if resp.GetNextPageToken() == "" {
		t.Fatal("next_page_token is empty, want a FILES-phase resume token")
	}
	cur, err := decodeCursor(resp.GetNextPageToken())
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if cur.GetPhase() != cursorv1.ListPhase_LIST_PHASE_FILES || cur.GetLastId() != 0 {
		t.Errorf("cursor = %+v, want phase=FILES lastId=0 (no cursor yet)", cur)
	}
}

func TestListDirectoryResumeFilesPhase(t *testing.T) {
	token := encodeCursor(newDirFilesCursor(pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_ASC, "docs/", testFile(9, "docs/z.txt")))

	queries := NewMockFileIndex(t)
	queries.EXPECT().GetIndexQueueStatuses(mock.Anything, mock.Anything).Return(nil, nil)
	// Directories phase must not run again once a FILES-phase token exists.
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, db.ListFilesByKeyAscParams{
		KeyPattern: "docs/%",
		DirectOnly: true,
		DirPrefix:  "docs/",
		HasCursor:  true,
		LastKey:    "docs/z.txt",
		LastID:     9,
		PageLimit:  3,
	}).Return([]db.FileInfo{testFile(10, "docs/zz.txt")}, nil)

	srv := SearchServer{queries: queries}

	resp, err := srv.ListDirectory(context.Background(), &pb.ListDirectoryRequest{
		Path: "docs/", PageSize: 2, PageToken: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetDirectories()) != 0 {
		t.Errorf("directories = %v, want none", resp.GetDirectories())
	}
	if len(resp.GetFiles()) != 1 || resp.GetFiles()[0].GetKey() != "docs/zz.txt" {
		t.Errorf("files = %+v, want [docs/zz.txt]", resp.GetFiles())
	}
}

func TestListDirectoryEmpty(t *testing.T) {
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListChildDirectories(mock.Anything, mock.Anything).Return(nil, nil)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, mock.Anything).Return(nil, nil)

	srv := SearchServer{queries: queries}

	resp, err := srv.ListDirectory(context.Background(), &pb.ListDirectoryRequest{Path: "empty/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.GetDirectories()) != 0 || len(resp.GetFiles()) != 0 || resp.GetNextPageToken() != "" {
		t.Errorf("resp = %+v, want fully empty", resp)
	}
}

func TestListDirectoryPathNormalized(t *testing.T) {
	// A leading "/" is stripped and a trailing "/" is added.
	queries := NewMockFileIndex(t)
	queries.EXPECT().ListChildDirectories(mock.Anything, db.ListChildDirectoriesParams{
		Parent:    "docs/",
		PageLimit: defaultPageSize + 1,
	}).Return(nil, nil)
	queries.EXPECT().ListFilesByKeyAsc(mock.Anything, mock.Anything).Return(nil, nil)

	srv := SearchServer{queries: queries}

	if _, err := srv.ListDirectory(context.Background(), &pb.ListDirectoryRequest{Path: "/docs"}); err != nil {
		t.Fatal(err)
	}
}

func TestListDirectoryInvalidPath(t *testing.T) {
	srv := SearchServer{queries: NewMockFileIndex(t)}

	_, err := srv.ListDirectory(context.Background(), &pb.ListDirectoryRequest{Path: "docs/../etc/"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestListDirectoryPageTokenPathMismatch(t *testing.T) {
	token := encodeCursor(newDirCursor(pb.SortField_SORT_FIELD_KEY, pb.SortOrder_SORT_ORDER_ASC, "docs/", ""))

	srv := SearchServer{queries: NewMockFileIndex(t)}

	_, err := srv.ListDirectory(context.Background(), &pb.ListDirectoryRequest{Path: "other/", PageToken: token})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("error = %v, want InvalidArgument", err)
	}
}

func TestHasDotDotSegment(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"", false},
		{"archive..2026.zip", false},
		{"a/archive..zip", false},
		{"..", true},
		{"../a", true},
		{"a/..", true},
		{"a/../b", true},
	}
	for _, tt := range tests {
		if got := hasDotDotSegment(tt.s); got != tt.want {
			t.Errorf("hasDotDotSegment(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestNormalizeDirPath(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"docs", "docs/", false},
		{"docs/", "docs/", false},
		{"/docs/", "docs/", false},
		{"docs/../etc/", "", true},
		// ".." as a substring, not a whole path segment, is a legitimate name.
		{"archive..2026/", "archive..2026/", false},
	}
	for _, tt := range tests {
		got, err := normalizeDirPath(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("normalizeDirPath(%q): want error, got nil", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeDirPath(%q): unexpected error %v", tt.in, err)
		}
		if got != tt.want {
			t.Errorf("normalizeDirPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
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
		Return([]db.FileInfo{testFile(7, "obj-key")}, nil)
	queries.EXPECT().GetIndexQueueStatuses(mock.Anything, mock.Anything).Return(nil, nil)

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

func TestServeValidation(t *testing.T) {
	// No EXPECT calls: validation must reject before the handler runs.
	queries := NewMockFileIndex(t)

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

	_, err = client.ListFiles(ctx, &pb.ListFilesRequest{PageSize: -1}, grpc.WaitForReady(true))
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("ListFiles(PageSize:-1) code = %v, want InvalidArgument", status.Code(err))
	}

	_, err = client.ListFiles(ctx, &pb.ListFilesRequest{SortField: 99}, grpc.WaitForReady(true))
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("ListFiles(SortField:99) code = %v, want InvalidArgument", status.Code(err))
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
