package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"musiclub/internal/db"
)

func newClubs(t *testing.T) *Clubs {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return NewClubs(d)
}

func TestClubsCreateAndLookup(t *testing.T) {
	s, ctx := newClubs(t), context.Background()

	c, err := s.Create(ctx, "friday-friends", "  Friday FRIENDS ")
	require.NoError(t, err)
	require.Equal(t, "friday friends", c.Name)
	require.Equal(t, 14, c.SubmissionDays)
	require.Equal(t, 14, c.VotingDays)

	got, err := s.BySlug(ctx, "friday-friends")
	require.NoError(t, err)
	require.Equal(t, c.ID, got.ID)
	_, err = s.BySlug(ctx, "nope")
	require.ErrorIs(t, err, ErrNotFound)
}

func TestClubsCreateValidation(t *testing.T) {
	s, ctx := newClubs(t), context.Background()

	_, err := s.Create(ctx, "one", "One")
	require.NoError(t, err)
	cases := []struct {
		slug, name string
		want       error
	}{
		{"one", "Again", ErrSlugTaken},
		{"Bad Slug", "x", ErrInvalidSlug},
		{"", "x", ErrInvalidSlug},
		{"-lead", "x", ErrInvalidSlug},
		{"fine", "  ", ErrNameRequired},
	}
	for _, c := range cases {
		_, err := s.Create(ctx, c.slug, c.name)
		require.ErrorIs(t, err, c.want, "Create(%q, %q)", c.slug, c.name)
	}
}

func TestClubsList(t *testing.T) {
	s, ctx := newClubs(t), context.Background()
	for slug, name := range map[string]string{"b": "Beta", "a": "Alpha"} {
		_, err := s.Create(ctx, slug, name)
		require.NoError(t, err)
	}

	got, err := s.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "a", got[0].Slug)
}
