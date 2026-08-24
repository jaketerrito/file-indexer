//go:build integration

package db

import (
	"context"
	"testing"
	"time"
)

// TestFilesKeyByteOrderCollation asserts files.key sorts in raw byte order
// (COLLATE "C"), not the database's default locale-aware collation. This
// matters for two independent reasons (see migrations/001_initial.sql):
// agreement with S3's ListObjectsV2 listing order, and correctness of the
// directory-browsing loose index scan's skip trick (ListChildPrefixes),
// which assumes '/' (0x2F) sorts immediately before '0' (0x30).
//
// A locale-aware collation (e.g. en_US.UTF-8) typically sorts punctuation
// and case differently from raw bytes — "Z" before "a" here would fail under
// most locale collations, which case-fold or otherwise reorder letters
// relative to their byte values.
func TestFilesKeyByteOrderCollation(t *testing.T) {
	conn := testConn(t)
	q := New(conn)
	ctx := context.Background()
	prefix := uniqueKey(t) + "/"

	// Byte order: '/' (0x2F) < 'Z' (0x5A) < 'a' (0x61). A case-insensitive or
	// locale-aware collation commonly interleaves case instead.
	insertTestFile(t, conn, prefix+"Z.txt")
	insertTestFile(t, conn, prefix+"a.txt")

	asc, err := q.ListFilesByKeyAsc(ctx, ListFilesByKeyAscParams{
		KeyPattern: prefix + "%",
		PageLimit:  10,
	})
	if err != nil {
		t.Fatalf("ListFilesByKeyAsc: %v", err)
	}
	assertKeys(t, asc, prefix+"Z.txt", prefix+"a.txt")
}

// TestFilesKeyCollationIsC queries pg_collation via the information schema
// to directly assert the column's collation, rather than only inferring it
// indirectly from sort order.
func TestFilesKeyCollationIsC(t *testing.T) {
	conn := testConn(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var collation string
	err := conn.QueryRow(ctx, `
		SELECT co.collname
		FROM pg_attribute a
		JOIN pg_collation co ON co.oid = a.attcollation
		WHERE a.attrelid = 'files'::regclass AND a.attname = 'key'
	`).Scan(&collation)
	if err != nil {
		t.Fatalf("query files.key collation: %v", err)
	}
	if collation != "C" {
		t.Errorf("files.key collation = %q, want \"C\"", collation)
	}
}
