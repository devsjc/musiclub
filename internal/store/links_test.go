package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/db"
	"musiclub/internal/linkresolver"
)

func TestLinkCache(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	l := NewLinks(database)
	ctx := context.Background()

	matches := []linkresolver.Match{
		{Service: "youtube_music", Title: "Song 1", Artist: "Queen", URL: "https://music.youtube.com/watch?v=123"},
	}

	// Cache miss
	got, err := l.GetLinkCache(ctx, "youtube_music", "queen|song 1", time.Hour)
	require.NoError(t, err)
	require.Nil(t, got)

	// Save
	require.NoError(t, l.SetLinkCache(ctx, "youtube_music", "queen|song 1", matches))

	// Cache hit
	got, err = l.GetLinkCache(ctx, "youtube_music", "queen|song 1", time.Hour)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Queen", got[0].Artist)
	require.Equal(t, "Song 1", got[0].Title)

	// Expired TTL (zero duration maxAge)
	got, err = l.GetLinkCache(ctx, "youtube_music", "queen|song 1", -time.Second)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestSubmissionLinks(t *testing.T) {
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	ctx := context.Background()
	clubs := NewClubs(database)
	players := NewPlayers(database)
	rounds := NewRounds(database)
	subs := NewSubmissions(database)
	links := NewLinks(database)

	club, err := clubs.Create(ctx, "test", "Test")
	require.NoError(t, err)
	p, err := players.Create(ctx, club.ID, "Sam", true)
	require.NoError(t, err)
	r, err := rounds.Create(ctx, club.ID, "2026-03", func(ids []int64) int64 { return ids[0] })
	require.NoError(t, err)
	require.NoError(t, rounds.SetTheme(ctx, club.ID, r.ID, p.ID, "Theme", "", time.Now()))

	sub, err := subs.Create(ctx, club.ID, r.ID, p.ID, Song{
		Title: "Song", Artist: "Artist", URL: "https://example.com/song", URLService: "other", LinkSource: "manual",
	}, time.Now())
	require.NoError(t, err)

	require.NoError(t, links.AddSubmissionLink(ctx, sub.ID, "spotify", "https://open.spotify.com/track/123", "123"))

	got, err := links.GetSubmissionLinks(ctx, sub.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "spotify", got[0].Service)
	require.Equal(t, "https://open.spotify.com/track/123", got[0].URL)
	require.Equal(t, "123", got[0].ExternalID)
}
