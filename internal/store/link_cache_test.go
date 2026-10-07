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

	c := NewLinkCache(database)
	ctx := context.Background()

	matches := []linkresolver.Match{
		{ExternalID: "123", Title: "Song 1", Artist: "Queen", URL: "https://music.youtube.com/watch?v=123"},
	}

	got, err := c.Get(ctx, "ytmusic", "queen|song 1", time.Hour)
	require.NoError(t, err)
	require.Nil(t, got, "miss")

	require.NoError(t, c.Set(ctx, "ytmusic", "queen|song 1", matches))

	got, err = c.Get(ctx, "ytmusic", "queen|song 1", time.Hour)
	require.NoError(t, err)
	require.Equal(t, matches, got, "hit")

	got, err = c.Get(ctx, "other", "queen|song 1", time.Hour)
	require.NoError(t, err)
	require.Nil(t, got, "another provider's key")

	got, err = c.Get(ctx, "ytmusic", "queen|song 1", -time.Second)
	require.NoError(t, err)
	require.Nil(t, got, "expired")
}
