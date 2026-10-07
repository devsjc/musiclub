// Package rounds is the lifecycle service: it starts rounds, picks and
// re-rolls the theme picker, takes the theme and the players' songs. Later
// stages add the phase transitions that follow.
package rounds

import (
	"context"
	"database/sql"
	"errors"
	"math/rand/v2"
	"strings"
	"time"
	"unicode/utf8"

	"musiclub/internal/domain"
	"musiclub/internal/linkresolver"
	"musiclub/internal/store"
)

const (
	maxTheme       = 100
	maxDescription = 500
)

var (
	ErrInvalidMonth       = errors.New("month must look like 2026-03")
	ErrThemeRequired      = errors.New("a theme is required")
	ErrThemeTooLong       = errors.New("the theme must be at most 100 characters")
	ErrDescriptionTooLong = errors.New("the description must be at most 500 characters")
)

type Service struct {
	rounds   *store.Rounds
	subs     *store.Submissions
	ballots  *store.Ballots
	quotes   *store.Quotes
	resolver *linkresolver.Resolver
	now      func() time.Time
	intn     func(n int) int
	shuffle  func(n int, swap func(i, j int))
}

func New(db *sql.DB, resolver *linkresolver.Resolver) *Service {
	return &Service{
		rounds:   store.NewRounds(db),
		subs:     store.NewSubmissions(db),
		ballots:  store.NewBallots(db),
		quotes:   store.NewQuotes(db),
		resolver: resolver,
		now:      time.Now,
		intn:     rand.IntN,
		shuffle:  rand.Shuffle,
	}
}

// StartRound creates the club's round for month ("YYYY-MM") and chooses its
// theme picker at random (DESIGN.md §5.4).
func (s *Service) StartRound(ctx context.Context, clubID int64, month string) (domain.Round, error) {
	if t, err := time.Parse("2006-01", month); err != nil || t.Format("2006-01") != month {
		return domain.Round{}, ErrInvalidMonth
	}
	return s.rounds.Create(ctx, clubID, month, s.choose)
}

// RerollPicker picks someone else to set the theme. Only valid while the round
// is theme_pending.
func (s *Service) RerollPicker(ctx context.Context, clubID, roundID int64) (domain.Round, error) {
	return s.rounds.Reroll(ctx, clubID, roundID, s.choose)
}

// SetTheme records the picker's theme and opens the submission window.
func (s *Service) SetTheme(ctx context.Context, clubID, roundID, pickerID int64, theme, description string) error {
	theme, description = strings.TrimSpace(theme), strings.TrimSpace(description)
	switch {
	case theme == "":
		return ErrThemeRequired
	case utf8.RuneCountInString(theme) > maxTheme:
		return ErrThemeTooLong
	case utf8.RuneCountInString(description) > maxDescription:
		return ErrDescriptionTooLong
	}
	return s.rounds.SetTheme(ctx, clubID, roundID, pickerID, theme, description, s.now())
}

func (s *Service) choose(ids []int64) int64 { return ids[s.intn(len(ids))] }

// Tick advances any rounds that have met their phase transition conditions
// (DESIGN.md §4). It is idempotent and safe to run concurrently.
func (s *Service) Tick(ctx context.Context, now time.Time) error {
	return s.rounds.AdvancePhases(ctx, now, s.shuffle)
}
