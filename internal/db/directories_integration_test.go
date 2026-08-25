//go:build integration

package db

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPool returns a pgxpool.Pool to the test database, for Store's
// transactional methods (Queries' plain testConn is a single *pgx.Conn,
// which cannot open transactions the way Store needs to).
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), testDSN(t))
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// directoryPathsUnder returns every directories row whose path starts with
// prefix, for tests that need to inspect a whole subtree rather than a
// single ListChildDirectories page. Scoped by prefix (rather than the whole
// table) so it isn't affected by PruneOrphanDirectories' unscoped cleanup of
// other tests' directory rows that were never backed by a real files row
// (several tests above call UpsertDirectoriesForKeys directly, bypassing
// files entirely, which is deliberately orphaned as far as the real
// invariant is concerned).
func directoryPathsUnder(t *testing.T, conn *pgx.Conn, prefix string) []string {
	t.Helper()
	rows, err := conn.Query(context.Background(),
		`SELECT path FROM directories WHERE path LIKE $1 ORDER BY path`, prefix+"%")
	if err != nil {
		t.Fatalf("query directories: %v", err)
	}
	defer rows.Close()
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan: %v", err)
		}
		paths = append(paths, p)
	}
	return paths
}

func TestUpsertDirectoriesForKeysCreatesFullAncestorChain(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	if err := q.UpsertDirectoriesForKeys(ctx, []string{prefix + "a/b/c/file.txt"}); err != nil {
		t.Fatalf("UpsertDirectoriesForKeys: %v", err)
	}

	children, err := q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories(root): %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"a/" {
		t.Fatalf("children of %q = %v, want [%sa/]", prefix, children, prefix)
	}

	children, err = q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix + "a/", PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories(a/): %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"a/b/" {
		t.Fatalf("children of a/ = %v, want [%sa/b/]", children, prefix)
	}

	children, err = q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix + "a/b/", PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories(a/b/): %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"a/b/c/" {
		t.Fatalf("children of a/b/ = %v, want [%sa/b/c/]", children, prefix)
	}

	// The leaf directory itself has no subdirectories (file.txt is a file).
	children, err = q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix + "a/b/c/", PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories(a/b/c/): %v", err)
	}
	if len(children) != 0 {
		t.Errorf("children of a/b/c/ = %v, want none", children)
	}
}

func TestUpsertDirectoriesForKeysSiblingNoDuplicate(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	if err := q.UpsertDirectoriesForKeys(ctx, []string{
		prefix + "docs/a.txt",
		prefix + "docs/b.txt",
	}); err != nil {
		t.Fatalf("UpsertDirectoriesForKeys: %v", err)
	}

	children, err := q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories: %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"docs/" {
		t.Fatalf("children = %v, want a single [%sdocs/] despite two files", children, prefix)
	}
}

func TestUpsertDirectoriesForKeysExcludesMarkerKeys(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	// A zero-byte marker at exactly a directory's own key must not produce
	// a bogus empty-named child one level down.
	if err := q.UpsertDirectoriesForKeys(ctx, []string{prefix + "sub/"}); err != nil {
		t.Fatalf("UpsertDirectoriesForKeys: %v", err)
	}

	children, err := q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix + "sub/", PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("children of sub/ = %v, want none (marker key must not create a child)", children)
	}
}

func TestUpsertDirectoriesForKeysBareFileNoAncestors(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t)

	// A top-level file's own name is not a directory; prefix itself (the
	// unique root segment from uniqueKey) is the only ancestor and it lives
	// at the bucket root, so nothing should be created here at all for a
	// key with zero '/' below the point being tested. We simulate "no
	// slash at all" by testing directly at the bucket root using a
	// separate, single-segment key.
	bare := prefix + "-top.txt"
	if err := q.UpsertDirectoriesForKeys(ctx, []string{bare}); err != nil {
		t.Fatalf("UpsertDirectoriesForKeys: %v", err)
	}

	children, err := q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: "", PageLimit: 1000})
	if err != nil {
		t.Fatalf("ListChildDirectories(root): %v", err)
	}
	for _, c := range children {
		if c == bare+"/" {
			t.Fatalf("bare top-level file must not create a directory, got %q", c)
		}
	}
}

func TestPruneDirectoriesForKeysRemovesEmptyAncestor(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"
	key := prefix + "sub/leaf.txt"

	if err := q.UpsertDirectoriesForKeys(ctx, []string{key}); err != nil {
		t.Fatalf("UpsertDirectoriesForKeys: %v", err)
	}
	if err := q.PruneDirectoriesForKeys(ctx, []string{key}); err != nil {
		t.Fatalf("PruneDirectoriesForKeys: %v", err)
	}

	children, err := q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("children = %v, want none (sub/ has no files left, in or under it)", children)
	}
}

func TestPruneDirectoriesForKeysKeepsAncestorWithSurvivingSibling(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	if err := q.UpsertDirectoriesForKeys(ctx, []string{
		prefix + "docs/a.txt",
		prefix + "docs/b.txt",
	}); err != nil {
		t.Fatalf("UpsertDirectoriesForKeys: %v", err)
	}

	// Only a.txt is "deleted" (files table not involved here — prune reads
	// files directly, so we insert b.txt as a real row to make the subtree
	// genuinely non-empty).
	insertTestFile(t, conn, prefix+"docs/b.txt")

	if err := q.PruneDirectoriesForKeys(ctx, []string{prefix + "docs/a.txt"}); err != nil {
		t.Fatalf("PruneDirectoriesForKeys: %v", err)
	}

	children, err := q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories: %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"docs/" {
		t.Fatalf("children = %v, want [%sdocs/] to survive (b.txt still there)", children, prefix)
	}
}

func TestPruneDirectoriesForKeysKeepsAncestorWithDeeperDescendant(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	if err := q.UpsertDirectoriesForKeys(ctx, []string{
		prefix + "a/b/deep.txt",
		prefix + "a/top.txt",
	}); err != nil {
		t.Fatalf("UpsertDirectoriesForKeys: %v", err)
	}
	// Keep only the deep file as a real files row.
	insertTestFile(t, conn, prefix+"a/b/deep.txt")

	if err := q.PruneDirectoriesForKeys(ctx, []string{prefix + "a/top.txt"}); err != nil {
		t.Fatalf("PruneDirectoriesForKeys: %v", err)
	}

	// "a/" must survive: a/b/deep.txt still falls in its subtree even
	// though a/top.txt (a direct child) is gone.
	children, err := q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories(root): %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"a/" {
		t.Fatalf("children = %v, want [%sa/] to survive (a/b/deep.txt still there)", children, prefix)
	}

	children, err = q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix + "a/", PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories(a/): %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"a/b/" {
		t.Fatalf("children of a/ = %v, want [%sa/b/]", children, prefix)
	}
}

func TestPruneOrphanDirectoriesUnscoped(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	// A directory row with nothing under it — simulates drift (e.g. a file
	// deleted through some path that skipped Store's WithDirectories
	// wrapper) rather than the normal maintained state.
	if err := q.UpsertDirectoriesForKeys(ctx, []string{prefix + "orphan/marker.txt"}); err != nil {
		t.Fatalf("UpsertDirectoriesForKeys: %v", err)
	}

	if err := q.PruneOrphanDirectories(ctx); err != nil {
		t.Fatalf("PruneOrphanDirectories: %v", err)
	}

	children, err := q.ListChildDirectories(ctx, ListChildDirectoriesParams{Parent: prefix, PageLimit: 10})
	if err != nil {
		t.Fatalf("ListChildDirectories: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("children = %v, want none (orphan/ has no backing file)", children)
	}
}

func TestListChildDirectoriesKeysetPagination(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	names := []string{"a", "b", "c", "d", "e"}
	keys := make([]string, len(names))
	for i, n := range names {
		keys[i] = prefix + n + "/f.txt"
	}
	if err := q.UpsertDirectoriesForKeys(ctx, keys); err != nil {
		t.Fatalf("UpsertDirectoriesForKeys: %v", err)
	}

	var got []string
	after := ""
	hasCursor := false
	for {
		page, err := q.ListChildDirectories(ctx, ListChildDirectoriesParams{
			Parent:    prefix,
			HasCursor: hasCursor,
			After:     after,
			PageLimit: 2,
		})
		if err != nil {
			t.Fatalf("ListChildDirectories: %v", err)
		}
		if len(page) == 0 {
			break
		}
		got = append(got, page...)
		after = page[len(page)-1]
		hasCursor = true
		if len(page) < 2 {
			break
		}
	}

	want := make([]string, len(names))
	for i, n := range names {
		want[i] = prefix + n + "/"
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDirectoriesConsistencyAfterRandomMutations is the test that actually
// catches maintenance bugs: it performs a randomized sequence of file
// creates/deletes through Store's WithDirectories wrappers (the only
// production write path — see FileIndex/FileStore's narrowed interfaces),
// then compares the resulting directories rows against an expected set
// computed independently in Go from the keys still believed live.
//
// The expected set is deliberately computed by plain string splitting, not
// by re-running any of UpsertDirectoriesForKeys/PruneDirectoriesForKeys'
// SQL — comparing incremental output against a second invocation of the
// *same* SQL (as an earlier version of this test did, via a since-removed
// RebuildDirectories) can only catch a missed call, never a bug in the
// shared ancestor-explosion logic itself, since both sides would compute
// the identical wrong answer. An independent oracle can.
func TestDirectoriesConsistencyAfterRandomMutations(t *testing.T) {
	pool := testPool(t)
	conn := testConn(t)
	store := NewStore(pool)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	rng := rand.New(rand.NewSource(1))
	segments := []string{"a", "b", "c"}
	var live []string // keys currently believed to exist

	randomKey := func() string {
		depth := rng.Intn(3) + 1
		key := prefix
		for i := 0; i < depth; i++ {
			key += segments[rng.Intn(len(segments))] + "/"
		}
		return key + fmt.Sprintf("file-%d.txt", rng.Intn(20))
	}

	const steps = 200
	for i := 0; i < steps; i++ {
		if len(live) == 0 || rng.Intn(2) == 0 {
			key := randomKey()
			if _, err := store.UpsertFilesWithDirectories(ctx, UpsertFilesParams{
				Keys:      []string{key},
				MarkedAts: []pgtype.Timestamptz{{Time: time.Now().UTC(), Valid: true}},
			}); err != nil {
				t.Fatalf("UpsertFilesWithDirectories: %v", err)
			}
			found := false
			for _, k := range live {
				if k == key {
					found = true
					break
				}
			}
			if !found {
				live = append(live, key)
			}
		} else {
			idx := rng.Intn(len(live))
			key := live[idx]
			var id int64
			if err := conn.QueryRow(ctx, `SELECT id FROM files WHERE key = $1`, key).Scan(&id); err != nil {
				t.Fatalf("lookup id for %q: %v", key, err)
			}
			if _, err := store.DeleteFileWithDirectories(ctx, id); err != nil {
				t.Fatalf("DeleteFileWithDirectories: %v", err)
			}
			live = append(live[:idx], live[idx+1:]...)
		}
	}

	want := wantDirectorySet(live, prefix)
	assertDirectorySet(t, directoryPathsUnder(t, conn, prefix), want)

	// PruneOrphanDirectories must find nothing to do: every directory
	// created by the incremental path above is backed by at least one live
	// file, by construction of `want` from the same `live` list.
	if err := store.PruneOrphanDirectories(ctx); err != nil {
		t.Fatalf("PruneOrphanDirectories: %v", err)
	}
	assertDirectorySet(t, directoryPathsUnder(t, conn, prefix), want)
}

// wantDirectorySet computes every ancestor directory implied by keys that
// falls at or under prefix, independently of any SQL: plain string
// splitting on the full key, mirroring only the semantics (every proper
// ancestor prefix, root not stored) rather than the implementation.
// Ancestors are computed from the full key, not keys with prefix trimmed
// off — prefix itself is a real directory too (it has children) — then
// filtered to scope>=prefix so the result matches directoryPathsUnder's own
// scoping (which excludes shorter ancestors like "it/" that other tests in
// this package/dbtest database also produce).
func wantDirectorySet(keys []string, prefix string) map[string]bool {
	want := map[string]bool{}
	for _, key := range keys {
		segs := strings.Split(key, "/")
		cur := ""
		for i := 0; i < len(segs)-1; i++ {
			cur += segs[i] + "/"
			if strings.HasPrefix(cur, prefix) {
				want[cur] = true
			}
		}
	}
	return want
}

func assertDirectorySet(t *testing.T, got []string, want map[string]bool) {
	t.Helper()
	gotSet := make(map[string]bool, len(got))
	for _, g := range got {
		gotSet[g] = true
	}
	if len(gotSet) != len(want) {
		t.Errorf("got %d directories, want %d", len(gotSet), len(want))
	}
	for g := range gotSet {
		if !want[g] {
			t.Errorf("unexpected directory %q", g)
		}
	}
	for w := range want {
		if !gotSet[w] {
			t.Errorf("missing directory %q", w)
		}
	}
	if t.Failed() {
		gotSorted := append([]string(nil), got...)
		sort.Strings(gotSorted)
		wantSorted := make([]string, 0, len(want))
		for w := range want {
			wantSorted = append(wantSorted, w)
		}
		sort.Strings(wantSorted)
		t.Logf("got:  %v", gotSorted)
		t.Logf("want: %v", wantSorted)
	}
}
