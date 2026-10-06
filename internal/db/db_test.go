package db

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// A file database is needed here: in-memory databases can't use WAL.
func TestOpenFileUsesWAL(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "club.db"))
	require.NoError(t, err)
	defer d.Close()

	var mode string
	var timeout int
	require.NoError(t, d.QueryRow(`PRAGMA journal_mode`).Scan(&mode))
	require.Equal(t, "wal", mode)
	require.NoError(t, d.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout))
	require.Equal(t, 5000, timeout)
}

func TestOpenAppliesMigrationsAndPragmas(t *testing.T) {
	d, err := Open(":memory:")
	require.NoError(t, err)
	defer d.Close()

	var fk int
	require.NoError(t, d.QueryRow(`PRAGMA foreign_keys`).Scan(&fk))
	require.Equal(t, 1, fk)

	var n int
	require.NoError(t, d.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('clubs','players','rounds','submissions','ballots','guesses','score_events')`).Scan(&n))
	require.Equal(t, 7, n, "core tables")
}

func TestMigrateIsIdempotent(t *testing.T) {
	d, err := Open(":memory:")
	require.NoError(t, err)
	defer d.Close()

	require.NoError(t, Migrate(d), "second Migrate")
	var n int
	require.NoError(t, d.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n))
	require.Equal(t, 1, n, "schema_migrations rows")
}
