package rounds

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/db"
	"musiclub/internal/domain"
	"musiclub/internal/store"
)

func newService(t *testing.T) (*Service, domain.Club, domain.Player, *sql.DB) {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	ctx := context.Background()
	club, err := store.NewClubs(d).Create(ctx, "friends", "Friends")
	require.NoError(t, err)
	sam, err := store.NewPlayers(d).Create(ctx, club.ID, "sam", true)
	require.NoError(t, err)
	s := New(d)
	s.now = func() time.Time { return time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC) }
	return s, club, sam, d
}

func TestStartRoundValidatesMonth(t *testing.T) {
	s, club, _, _ := newService(t)
	for _, m := range []string{"", "2026-13", "2026-1", "March", "2026-03-01", " 2026-03"} {
		_, err := s.StartRound(context.Background(), club.ID, m)
		require.ErrorIs(t, err, ErrInvalidMonth, "StartRound(%q)", m)
	}
	r, err := s.StartRound(context.Background(), club.ID, "2026-03")
	require.NoError(t, err)
	require.Equal(t, "2026-03", r.Month)
}

func TestStartRoundUsesInjectedChoice(t *testing.T) {
	s, club, _, _ := newService(t)
	calls := 0
	s.intn = func(n int) int { calls++; return n - 1 }
	_, err := s.StartRound(context.Background(), club.ID, "2026-03")
	require.NoError(t, err)
	require.Equal(t, 1, calls)
}

func TestSetThemeValidation(t *testing.T) {
	s, club, sam, _ := newService(t)
	ctx := context.Background()
	r, err := s.StartRound(ctx, club.ID, "2026-03")
	require.NoError(t, err)

	for _, c := range []struct {
		theme, desc string
		want        error
	}{
		{"", "", ErrThemeRequired},
		{"   ", "", ErrThemeRequired},
		{strings.Repeat("x", maxTheme+1), "", ErrThemeTooLong},
		{"Rain", strings.Repeat("x", maxDescription+1), ErrDescriptionTooLong},
	} {
		err := s.SetTheme(ctx, club.ID, r.ID, sam.ID, c.theme, c.desc)
		require.ErrorIs(t, err, c.want, "SetTheme(%.10q, %.10q)", c.theme, c.desc)
	}

	require.NoError(t, s.SetTheme(ctx, club.ID, r.ID, sam.ID, "  Rain ", " wet "))
	got, err := s.rounds.Get(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Equal(t, "Rain", got.Theme)
	require.Equal(t, "wet", got.ThemeDescription)
	require.Equal(t, domain.PhaseSubmitting, got.Phase)
	wantDue := time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC) // club default: 14 days
	require.NotNil(t, got.SubmissionsDueAt)
	require.True(t, got.SubmissionsDueAt.Equal(wantDue), "due = %v, want %v", got.SubmissionsDueAt, wantDue)
}
