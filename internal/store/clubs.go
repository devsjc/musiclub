// Package store holds all SQL. Functions touching club data take a clubID.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"musiclub/internal/domain"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrSlugTaken    = errors.New("club slug already exists")
	ErrInvalidSlug  = errors.New("slug must be lowercase letters, digits and hyphens, e.g. friday-friends")
	ErrNameRequired = errors.New("club name is required")
	slugPattern     = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

type Clubs struct{ db *sql.DB }

func NewClubs(db *sql.DB) *Clubs { return &Clubs{db: db} }

// Create inserts a new club with the default window lengths.
func (s *Clubs) Create(ctx context.Context, slug, name string) (domain.Club, error) {
	name = strings.TrimSpace(name)
	if !slugPattern.MatchString(slug) {
		return domain.Club{}, ErrInvalidSlug
	}
	if name == "" {
		return domain.Club{}, ErrNameRequired
	}

	res, err := s.db.ExecContext(ctx, `INSERT INTO clubs (slug, name) VALUES (?, ?)`, slug, name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: clubs.slug") {
			return domain.Club{}, ErrSlugTaken
		}
		return domain.Club{}, fmt.Errorf("insert club: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Club{}, err
	}
	return s.get(ctx, `id = ?`, id)
}

// BySlug returns the club with the given slug, or ErrNotFound.
func (s *Clubs) BySlug(ctx context.Context, slug string) (domain.Club, error) {
	return s.get(ctx, `slug = ?`, slug)
}

// List returns every club ordered by name. This is the one query that
// legitimately reads across clubs.
func (s *Clubs) List(ctx context.Context) ([]domain.Club, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, slug, name, submission_days, voting_days, created_at FROM clubs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Club
	for rows.Next() {
		c, err := scanClub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Clubs) get(ctx context.Context, where string, arg any) (domain.Club, error) {
	c, err := scanClub(s.db.QueryRowContext(ctx,
		`SELECT id, slug, name, submission_days, voting_days, created_at FROM clubs WHERE `+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Club{}, ErrNotFound
	}
	return c, err
}

func scanClub(r interface{ Scan(...any) error }) (domain.Club, error) {
	var c domain.Club
	var created string
	if err := r.Scan(&c.ID, &c.Slug, &c.Name, &c.SubmissionDays, &c.VotingDays, &created); err != nil {
		return domain.Club{}, err
	}
	t, err := parseTime(created)
	if err != nil {
		return domain.Club{}, err
	}
	c.CreatedAt = t
	return c, nil
}
