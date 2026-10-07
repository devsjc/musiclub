package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"musiclub/internal/linkresolver"
)

// LinkCache stores provider search responses in link_cache. Safe to wipe.
type LinkCache struct {
	db *sql.DB
}

func NewLinkCache(db *sql.DB) *LinkCache {
	return &LinkCache{db: db}
}

func (s *LinkCache) Get(ctx context.Context, provider, queryKey string, maxAge time.Duration) ([]linkresolver.Match, error) {
	var response string
	var fetchedStr string
	err := s.db.QueryRowContext(ctx, `
		SELECT response, fetched_at
		FROM link_cache
		WHERE provider = ? AND query_key = ?
	`, provider, queryKey).Scan(&response, &fetchedStr)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	fetchedAt, err := parseTime(fetchedStr)
	if err != nil {
		return nil, nil
	}

	if time.Since(fetchedAt) > maxAge {
		return nil, nil // Treat as expired (don't bother deleting, it will be overwritten)
	}

	var matches []linkresolver.Match
	if err := json.Unmarshal([]byte(response), &matches); err != nil {
		return nil, err
	}
	return matches, nil
}

func (s *LinkCache) Set(ctx context.Context, provider, queryKey string, matches []linkresolver.Match) error {
	data, err := json.Marshal(matches)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO link_cache (provider, query_key, response, fetched_at)
		VALUES (?, ?, ?, datetime('now'))
		ON CONFLICT(provider, query_key) DO UPDATE SET
			response = excluded.response,
			fetched_at = excluded.fetched_at
	`, provider, queryKey, data)
	return err
}
