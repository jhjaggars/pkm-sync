package main

import (
	"database/sql"
	"path/filepath"
	"testing"

	"pkm-sync/internal/vectorstore"
)

// resetNormalizeTimestampsFlags restores the package-level flag variables
// used by runNormalizeTimestamps so tests don't leak state into each other.
func resetNormalizeTimestampsFlags(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		normalizeTimestampsDBPath = ""
		normalizeTimestampsDryRun = false
	})
}

func TestRunNormalizeTimestamps_MissingFile(t *testing.T) {
	resetNormalizeTimestampsFlags(t)

	dir := t.TempDir()
	normalizeTimestampsDBPath = filepath.Join(dir, "does-not-exist.db")
	normalizeTimestampsDryRun = true

	if err := runNormalizeTimestamps(nil, nil); err == nil {
		t.Fatal("expected error opening a nonexistent vectors.db, got nil")
	}
}

// TestRunNormalizeTimestamps_DryRunThenApply builds a synthetic vectors.db
// (never a production file) with a mix of Z-suffixed and local-offset
// timestamps, then exercises the CLI wiring end to end: dry-run must report
// the row but write nothing, and a real run must normalize it.
func TestRunNormalizeTimestamps_DryRunThenApply(t *testing.T) {
	resetNormalizeTimestampsFlags(t)

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "vectors.db")

	// Build a synthetic vectors.db with the real schema, then seed a row with
	// a local-offset created_at directly via SQL — bypassing UpsertDocument,
	// which already writes the correct Z-suffixed form — to simulate pre-fix
	// production data.
	store, err := vectorstore.NewStore(dbPath, 0)
	if err != nil {
		t.Fatalf("failed to create synthetic store: %v", err)
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		store.Close()
		t.Fatalf("failed to open synthetic db for seeding: %v", err)
	}

	_, err = db.Exec(
		`INSERT INTO documents (source_id, thread_id, source_name, created_at, updated_at)
		 VALUES ('src1', 'thread-bad', 'gmail_work', ?, ?)`,
		"2026-07-11T15:30:13-04:00", "2026-07-11T15:30:13-04:00",
	)

	db.Close()
	store.Close()

	if err != nil {
		t.Fatalf("failed to seed row: %v", err)
	}

	readCreatedAt := func() string {
		t.Helper()

		db, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			t.Fatalf("failed to reopen synthetic db: %v", err)
		}
		defer db.Close()

		var raw string

		if err := db.QueryRow("SELECT created_at FROM documents WHERE thread_id = 'thread-bad'").Scan(&raw); err != nil {
			t.Fatalf("failed to read back row: %v", err)
		}

		return raw
	}

	normalizeTimestampsDBPath = dbPath
	normalizeTimestampsDryRun = true

	if err := runNormalizeTimestamps(nil, nil); err != nil {
		t.Fatalf("dry-run failed: %v", err)
	}

	if got := readCreatedAt(); got != "2026-07-11T15:30:13-04:00" {
		t.Fatalf("dry-run must not modify rows, got created_at = %q", got)
	}

	normalizeTimestampsDryRun = false

	if err := runNormalizeTimestamps(nil, nil); err != nil {
		t.Fatalf("apply run failed: %v", err)
	}

	if got, want := readCreatedAt(), "2026-07-11T19:30:13Z"; got != want {
		t.Fatalf("created_at = %q, want normalized %q", got, want)
	}

	// Idempotent: running again against the now-clean file is a no-op.
	if err := runNormalizeTimestamps(nil, nil); err != nil {
		t.Fatalf("second apply run failed: %v", err)
	}

	if got := readCreatedAt(); got != "2026-07-11T19:30:13Z" {
		t.Fatalf("second run must not change an already-normalized row, got %q", got)
	}
}
