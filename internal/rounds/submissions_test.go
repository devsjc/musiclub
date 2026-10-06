package rounds

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
	"musiclub/internal/linkresolver"
	"musiclub/internal/store"
)

func submittingRound(t *testing.T) (*Service, domain.Club, domain.Player, domain.Round) {
	t.Helper()
	s, club, sam, _ := newService(t)
	ctx := context.Background()
	r, err := s.StartRound(ctx, club.ID, "2026-03")
	require.NoError(t, err)
	require.NoError(t, s.SetTheme(ctx, club.ID, r.ID, sam.ID, "Rain", ""))
	return s, club, sam, r
}

func TestSubmitSongStoresCleanedSong(t *testing.T) {
	s, club, sam, r := submittingRound(t)

	got, err := s.SubmitSong(context.Background(), club.ID, r.ID, sam.ID, Song{
		Artist: "  Queen ", Title: " Bohemian Rhapsody (Remastered 2011) ", Note: " classic ",
		URL: " https://open.spotify.com/track/abc?si=tracking ",
	})
	require.NoError(t, err)
	require.Equal(t, "Queen", got.Artist)
	require.Equal(t, "Bohemian Rhapsody (Remastered 2011)", got.Title)
	require.Equal(t, "classic", got.Note)
	require.Equal(t, "https://open.spotify.com/track/abc", got.URL)
	require.Equal(t, "spotify", got.URLService)
	require.Equal(t, "manual", got.LinkSource)
	require.Equal(t, "queen|bohemian rhapsody", got.SongKey)
}

func TestSubmitSongValidation(t *testing.T) {
	s, club, sam, r := submittingRound(t)
	ok := Song{Artist: "Queen", Title: "Radio Ga Ga", URL: "https://example.com/x"}

	for _, c := range []struct {
		name   string
		mutate func(*Song)
		want   error
	}{
		{"no artist", func(s *Song) { s.Artist = " " }, ErrArtistRequired},
		{"no title", func(s *Song) { s.Title = "" }, ErrTitleRequired},
		{"long artist", func(s *Song) { s.Artist = strings.Repeat("x", maxSongField+1) }, ErrSongFieldLong},
		{"long title", func(s *Song) { s.Title = strings.Repeat("x", maxSongField+1) }, ErrSongFieldLong},
		{"long note", func(s *Song) { s.Note = strings.Repeat("x", maxNote+1) }, ErrNoteTooLong},
		{"punctuation only", func(s *Song) { s.Title = "???" }, ErrSongUnreadable},
		{"no url", func(s *Song) { s.URL = "" }, linkresolver.ErrURLRequired},
		{"javascript url", func(s *Song) { s.URL = "javascript:alert(1)" }, linkresolver.ErrURLInvalid},
	} {
		song := ok
		c.mutate(&song)
		_, err := s.SubmitSong(context.Background(), club.ID, r.ID, sam.ID, song)
		require.ErrorIs(t, err, c.want, c.name)
	}
	_, err := s.subs.ByPlayer(context.Background(), club.ID, r.ID, sam.ID)
	require.ErrorIs(t, err, store.ErrNotFound, "rejected submissions left a row")
}

func TestSubmitSongNeedsOpenWindow(t *testing.T) {
	s, club, sam, r := submittingRound(t)
	song := Song{Artist: "Queen", Title: "Radio Ga Ga", URL: "https://example.com/x"}
	ctx := context.Background()

	// newService fixes the clock at 2026-03-01 09:00 UTC; the window is 14 days.
	opened := s.now()
	s.now = func() time.Time { return opened.Add(14*24*time.Hour - time.Minute) }
	_, err := s.SubmitSong(ctx, club.ID, r.ID, sam.ID, song)
	require.NoError(t, err, "inside the window")
	s.now = func() time.Time { return opened.Add(14 * 24 * time.Hour) }
	_, err = s.SubmitSong(ctx, club.ID, r.ID, sam.ID, song)
	require.ErrorIs(t, err, store.ErrDeadlinePassed, "at the deadline")
}
