package store

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"musiclub/internal/db"
	"musiclub/internal/domain"
)

func newPlayers(t *testing.T) (*Players, domain.Club, domain.Club) {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	clubs := NewClubs(d)
	a, err := clubs.Create(context.Background(), "a", "A")
	require.NoError(t, err)
	b, err := clubs.Create(context.Background(), "b", "B")
	require.NoError(t, err)
	return NewPlayers(d), a, b
}

func TestPlayersCreateAndGet(t *testing.T) {
	s, club, _ := newPlayers(t)
	ctx := context.Background()

	p, err := s.Create(ctx, club.ID, "  SAM ", true)
	require.NoError(t, err)
	require.Equal(t, "sam", p.Name)
	require.True(t, p.Active)
	require.True(t, p.IsAdmin)
	require.Equal(t, club.ID, p.ClubID)

	q, err := s.Create(ctx, club.ID, "Kit", false)
	require.NoError(t, err)
	require.False(t, q.IsAdmin)
	got, err := s.List(ctx, club.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "kit", got[0].Name)
}

func TestPlayersCreateValidation(t *testing.T) {
	s, club, other := newPlayers(t)
	ctx := context.Background()

	_, err := s.Create(ctx, club.ID, "Sam", false)
	require.NoError(t, err)
	cases := []struct {
		clubID int64
		name   string
		want   error
	}{
		{club.ID, "sam", ErrPlayerNameTaken},
		{club.ID, "SAM", ErrPlayerNameTaken}, // case-insensitive
		{club.ID, "   ", ErrPlayerNameRequired},
		{club.ID, strings.Repeat("x", 51), ErrPlayerNameTooLong},
	}
	for _, c := range cases {
		_, err := s.Create(ctx, c.clubID, c.name, false)
		require.ErrorIs(t, err, c.want, "Create(%q)", c.name)
	}
	// The same name is fine in a different club.
	_, err = s.Create(ctx, other.ID, "Sam", false)
	require.NoError(t, err, "same name in other club")
}

func TestPlayersLastAdminGuard(t *testing.T) {
	s, club, _ := newPlayers(t)
	ctx := context.Background()

	admin, _ := s.Create(ctx, club.ID, "Admin", true)
	require.ErrorIs(t, s.SetAdmin(ctx, club.ID, admin.ID, false), ErrLastAdmin, "revoke last admin")
	require.ErrorIs(t, s.SetActive(ctx, club.ID, admin.ID, false), ErrLastAdmin, "retire last admin")

	second, _ := s.Create(ctx, club.ID, "Second", true)
	require.NoError(t, s.SetActive(ctx, club.ID, admin.ID, false), "retire with another admin")
	got, _ := s.Get(ctx, club.ID, admin.ID)
	require.False(t, got.Active, "player should be inactive")
	// The remaining admin is now the last one.
	require.ErrorIs(t, s.SetAdmin(ctx, club.ID, second.ID, false), ErrLastAdmin)
	// An inactive admin doesn't count, and reinstating is always allowed.
	require.NoError(t, s.SetActive(ctx, club.ID, admin.ID, true))
	require.NoError(t, s.SetAdmin(ctx, club.ID, second.ID, false), "revoke with another active admin")
}

// Two-club test (DESIGN.md §14): nothing leaks between clubs.
func TestPlayersAreClubScoped(t *testing.T) {
	s, a, b := newPlayers(t)
	ctx := context.Background()

	pa, _ := s.Create(ctx, a.ID, "Alice", true)
	pb, _ := s.Create(ctx, b.ID, "Bob", true)

	got, _ := s.List(ctx, a.ID)
	require.Len(t, got, 1)
	require.Equal(t, pa.ID, got[0].ID)
	_, err := s.Get(ctx, a.ID, pb.ID)
	require.ErrorIs(t, err, ErrNotFound, "Get other club's player")
	require.ErrorIs(t, s.SetActive(ctx, a.ID, pb.ID, false), ErrNotFound, "SetActive other club's player")
	require.ErrorIs(t, s.SetAdmin(ctx, a.ID, pb.ID, false), ErrNotFound, "SetAdmin other club's player")
	unchanged, _ := s.Get(ctx, b.ID, pb.ID)
	require.True(t, unchanged.Active && unchanged.IsAdmin, "other club's player was modified: %+v", unchanged)
}
