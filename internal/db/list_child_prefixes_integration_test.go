//go:build integration

package db

import (
	"context"
	"fmt"
	"testing"
)

func TestListChildPrefixesBasic(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	insertTestFile(t, conn, prefix+"a.txt")
	insertTestFile(t, conn, prefix+"sub/b.txt")
	insertTestFile(t, conn, prefix+"sub/c.txt")
	insertTestFile(t, conn, prefix+"sub2/d.txt")

	children, err := q.ListChildPrefixes(ctx, ListChildPrefixesParams{
		Prefix:        prefix,
		PrefixPattern: prefix + "%",
		After:         prefix,
		DirLimit:      10,
		ScanLimit:     1000,
	})
	if err != nil {
		t.Fatalf("ListChildPrefixes: %v", err)
	}
	want := []string{prefix + "sub/", prefix + "sub2/"}
	if len(children) != len(want) {
		t.Fatalf("got %v, want %v", children, want)
	}
	for i, w := range want {
		if children[i] != w {
			t.Errorf("children[%d] = %q, want %q", i, children[i], w)
		}
	}
}

func TestListChildPrefixesRoot(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	insertTestFile(t, conn, uniqueKey(t)+"/x.txt")

	// prefix="" is the literal bucket root: every key this package's tests
	// ever insert is uniqueKey-namespaced under "it/...", so regardless of
	// which other tests in this package have run, the root has exactly one
	// child, "it/".
	children, err := q.ListChildPrefixes(ctx, ListChildPrefixesParams{
		Prefix:        "",
		PrefixPattern: "%",
		After:         "",
		DirLimit:      10,
		ScanLimit:     1000,
	})
	if err != nil {
		t.Fatalf("ListChildPrefixes: %v", err)
	}
	if len(children) != 1 || children[0] != "it/" {
		t.Fatalf("children = %v, want [it/]", children)
	}
}

func TestListChildPrefixesPagination(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	dirs := []string{"a", "b", "c", "d", "e"}
	for _, d := range dirs {
		insertTestFile(t, conn, prefix+d+"/f.txt")
	}

	var got []string
	after := prefix
	for {
		page, err := q.ListChildPrefixes(ctx, ListChildPrefixesParams{
			Prefix:        prefix,
			PrefixPattern: prefix + "%",
			After:         after,
			DirLimit:      2,
			ScanLimit:     1000,
		})
		if err != nil {
			t.Fatalf("ListChildPrefixes: %v", err)
		}
		if len(page) == 0 {
			break
		}
		got = append(got, page...)
		last := page[len(page)-1]
		after = last[:len(last)-1] + "0" // skipPastChild, inlined
		if len(page) < 2 {
			break
		}
	}

	if len(got) != len(dirs) {
		t.Fatalf("got %v, want %d dirs from %v", got, len(dirs), dirs)
	}
	for i, d := range dirs {
		want := prefix + d + "/"
		if got[i] != want {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want)
		}
	}
}

func TestListChildPrefixesExcludesMarkerKeys(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	// A zero-byte marker object some external tool (e.g. the S3 console)
	// left behind at exactly the directory's own key. It must not surface
	// as an empty-named child, nor break the walk.
	insertTestFile(t, conn, prefix+"sub/")
	insertTestFile(t, conn, prefix+"sub/real.txt")

	children, err := q.ListChildPrefixes(ctx, ListChildPrefixesParams{
		Prefix:        prefix,
		PrefixPattern: prefix + "%",
		After:         prefix,
		DirLimit:      10,
		ScanLimit:     1000,
	})
	if err != nil {
		t.Fatalf("ListChildPrefixes: %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"sub/" {
		t.Fatalf("children = %v, want [%ssub/]", children, prefix)
	}
}

func TestListChildPrefixesScanLimitBounds(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	// Many top-level plain files sorting before the one subdirectory: with a
	// scan_limit smaller than the number of files, the walk must not find
	// the subdirectory (it isn't reachable within budget), and must not
	// error or hang either.
	const numFiles = 20
	for i := range numFiles {
		insertTestFile(t, conn, fmt.Sprintf("%sfile-%02d.txt", prefix, i))
	}
	insertTestFile(t, conn, prefix+"zzz-sub/only.txt")

	children, err := q.ListChildPrefixes(ctx, ListChildPrefixesParams{
		Prefix:        prefix,
		PrefixPattern: prefix + "%",
		After:         prefix,
		DirLimit:      10,
		ScanLimit:     5, // far fewer than numFiles
	})
	if err != nil {
		t.Fatalf("ListChildPrefixes: %v", err)
	}
	if len(children) != 0 {
		t.Errorf("children = %v, want none (subdirectory unreachable within scan_limit)", children)
	}

	// A generous scan_limit finds it.
	children, err = q.ListChildPrefixes(ctx, ListChildPrefixesParams{
		Prefix:        prefix,
		PrefixPattern: prefix + "%",
		After:         prefix,
		DirLimit:      10,
		ScanLimit:     numFiles + 10,
	})
	if err != nil {
		t.Fatalf("ListChildPrefixes: %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"zzz-sub/" {
		t.Fatalf("children = %v, want [%szzz-sub/]", children, prefix)
	}
}

func TestListChildPrefixesSkipsManyFilesInOneSubdirectory(t *testing.T) {
	// The actual point of the loose index scan: a subdirectory containing
	// many files must be discoverable (and skippable) in one page even with
	// a tight scan_limit, because the walk jumps past its whole key range in
	// a single step rather than visiting each file in it.
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	const numFiles = 500
	for i := range numFiles {
		insertTestFile(t, conn, fmt.Sprintf("%sbig/file-%03d.txt", prefix, i))
	}
	insertTestFile(t, conn, prefix+"zzz-after.txt")

	children, err := q.ListChildPrefixes(ctx, ListChildPrefixesParams{
		Prefix:        prefix,
		PrefixPattern: prefix + "%",
		After:         prefix,
		DirLimit:      10,
		ScanLimit:     5, // far fewer than numFiles: only reachable via the skip
	})
	if err != nil {
		t.Fatalf("ListChildPrefixes: %v", err)
	}
	if len(children) != 1 || children[0] != prefix+"big/" {
		t.Fatalf("children = %v, want [%sbig/] (found via one skip, not by visiting all %d files)", children, prefix, numFiles)
	}
}
