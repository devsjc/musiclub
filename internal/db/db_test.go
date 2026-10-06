package db

import (
	"path/filepath"
	"testing"
)

// A file database is needed here: in-memory databases can't use WAL.
func TestOpenFileUsesWAL(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "club.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var mode string
	var timeout int
	if err := d.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, err %v; want wal", mode, err)
	}
	if err := d.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 5000 {
		t.Fatalf("busy_timeout = %d, err %v; want 5000", timeout, err)
	}
}

func TestOpenAppliesMigrationsAndPragmas(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var fk int
	if err := d.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, err %v; want 1", fk, err)
	}

	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('clubs','players','rounds','submissions','ballots','guesses','score_events')`).Scan(&n); err != nil || n != 7 {
		t.Fatalf("expected core tables, got %d (err %v)", n, err)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	if err := Migrate(d); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	var n int
	if err := d.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", n)
	}
}
