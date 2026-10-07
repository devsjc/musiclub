package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"musiclub/internal/domain"
	"musiclub/internal/linkresolver"
)

type Links struct {
	db *sql.DB
}

func NewLinks(db *sql.DB) *Links {
	return &Links{db: db}
}

func (s *Links) GetLinkCache(ctx context.Context, provider, queryKey string, maxAge time.Duration) ([]linkresolver.Match, error) {
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

func (s *Links) SetLinkCache(ctx context.Context, provider, queryKey string, matches []linkresolver.Match) error {
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

func (s *Links) AddSubmissionLink(ctx context.Context, submissionID int64, service, url, externalID string) error {
	var extID interface{}
	if externalID != "" {
		extID = externalID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO submission_links (submission_id, service, url, external_id)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(submission_id, service) DO NOTHING
	`, submissionID, service, url, extID)
	return err
}

func (s *Links) GetSubmissionLinks(ctx context.Context, submissionID int64) ([]domain.SubmissionLink, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT submission_id, service, url, external_id, created_at
		FROM submission_links
		WHERE submission_id = ?
		ORDER BY service
	`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var links []domain.SubmissionLink
	for rows.Next() {
		var l domain.SubmissionLink
		var extID sql.NullString
		var created string
		if err := rows.Scan(&l.SubmissionID, &l.Service, &l.URL, &extID, &created); err != nil {
			return nil, err
		}
		l.ExternalID = extID.String
		// Ignoring created_at parse error for brevity, since it's just best-effort
		l.CreatedAt, _ = time.Parse(time.RFC3339, created)
		links = append(links, l)
	}
	return links, rows.Err()
}
