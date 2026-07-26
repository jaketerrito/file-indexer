// Package dbtest gives each integration-test package its own throwaway
// Postgres database. Without this, integration tests running in different
// packages (which go test runs concurrently) and any real indexer/crawler
// daemon polling the same shared dev database (e.g. under Tilt) all mutate
// the same files/index_queue rows. That has a real failure mode: an
// INSERT ... SELECT FROM files (e.g. SeedIndexQueue) can race a concurrent
// DELETE FROM files (test cleanup, or an unrelated test's teardown) between
// its SELECT and the FK check, producing sporadic
// index_queue_file_id_fkey violations. A private database per test binary
// removes the shared mutable state entirely instead of trying to name
// around it.
package dbtest

import (
	"context"
	"file-indexer/internal/config"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

var dsn string

// DSN returns the DSN of the temporary database created by Main. It is only
// valid for the duration of TestMain's m.Run() call.
func DSN() string {
	if dsn == "" {
		panic("dbtest: DSN called before Main (add TestMain(m) { os.Exit(dbtest.Main(m)) })")
	}
	return dsn
}

// Main creates a temporary database, runs migrations against it via
// migrate, runs the test suite, and drops the database. Call it from
// TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(dbtest.Main(m, db.RunMigrations)) }
//
// migrate takes db.RunMigrations directly; it is a parameter (rather than
// dbtest importing internal/db itself) so internal/db's own integration
// tests can use dbtest without an import cycle.
//
// Main requires DB_HOST (and friends) to point at an admin-capable Postgres
// connection, same as the rest of the integration suite; see
// internal/db/integration_test.go's testDSN for the exact variables. A
// process killed mid-run leaks its database (named it_<pid>_<nanotime>) —
// acceptable on the ephemeral dev cluster these tests run against; drop
// stragglers manually if it ever matters.
func Main(m *testing.M, migrate func(ctx context.Context, driver, dsn string) error) int {
	if os.Getenv("DB_HOST") == "" {
		fmt.Fprintln(os.Stderr, "dbtest: integration tests require DB_HOST to be set (see just test-integration)")
		return 1
	}

	adminCfg := config.Load().Database
	adminDSN := adminCfg.URL()

	ctx := context.Background()
	name := fmt.Sprintf("it_%d_%d", os.Getpid(), time.Now().UnixNano())

	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: connect to admin database: %v\n", err)
		return 1
	}

	// Database identifiers can't be bound parameters; name is our own
	// generated [a-z0-9_]+ string, never user input.
	if _, err := admin.Exec(ctx, `CREATE DATABASE `+name); err != nil {
		_ = admin.Close(ctx)
		fmt.Fprintf(os.Stderr, "dbtest: create database %s: %v\n", name, err)
		return 1
	}
	_ = admin.Close(ctx)

	testCfg := adminCfg
	testCfg.DBName = name
	dsn = testCfg.URL()

	if err := migrate(ctx, "pgx", dsn); err != nil {
		fmt.Fprintf(os.Stderr, "dbtest: run migrations against %s: %v\n", name, err)
		dropDatabase(adminDSN, name)
		return 1
	}

	code := m.Run()

	dropDatabase(adminDSN, name)
	return code
}

func dropDatabase(adminDSN, name string) {
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		slog.Error("dbtest: reconnect to admin database to drop test database", "database", name, "error", err)
		return
	}
	defer func() { _ = admin.Close(ctx) }()

	// WITH (FORCE) disconnects any lingering connections (e.g. a leaked
	// pgxpool) instead of failing the drop.
	if _, err := admin.Exec(ctx, `DROP DATABASE IF EXISTS `+name+` WITH (FORCE)`); err != nil {
		slog.Error("dbtest: drop test database", "database", name, "error", err)
	}
}
