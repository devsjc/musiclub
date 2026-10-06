package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
)

type subsEnv struct {
	*roundsEnv
	subs  *Submissions
	round domain.Round // submitting, due 2026-03-15 09:00 UTC
	sam   domain.Player
	kit   domain.Player
}

var t0 = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

func newSubs(t *testing.T) *subsEnv {
	t.Helper()
	e := newRounds(t)
	ctx := context.Background()
	sam, kit := e.player(t, e.club, "sam"), e.player(t, e.club, "kit")
	r, err := e.rounds.Create(ctx, e.club.ID, "2026-03", first)
	require.NoError(t, err)
	require.NoError(t, e.rounds.SetTheme(ctx, e.club.ID, r.ID, r.PickerID, "Rain", "", t0))
	r, err = e.rounds.Get(ctx, e.club.ID, r.ID)
	require.NoError(t, err)
	return &subsEnv{roundsEnv: e, subs: NewSubmissions(e.rounds.db), round: r, sam: sam, kit: kit}
}

func song(key string) Song {
	return Song{Title: "Title " + key, Artist: "Artist", URL: "https://example.com/" + key, URLService: "other", LinkSource: "manual", SongKey: key}
}

func TestSubmissionsCreateAndDuplicateError(t *testing.T) {
	e := newSubs(t)
	ctx := context.Background()

	s := song("a")
	s.Note = "hi"
	got, err := e.subs.Create(ctx, e.club.ID, e.round.ID, e.sam.ID, s, t0)
	require.NoError(t, err)
	require.Equal(t, e.sam.ID, got.PlayerID)
	require.Equal(t, e.round.ID, got.RoundID)
	require.Equal(t, "Title a", got.Title)
	require.Equal(t, "hi", got.Note)
	require.Equal(t, "other", got.URLService)
	require.Equal(t, "manual", got.LinkSource)
	require.Empty(t, got.ExternalID)
	require.Nil(t, got.Position)
	require.True(t, got.SubmittedAt.Equal(t0), "submitted_at = %v", got.SubmittedAt)

	// Creating again returns ErrAlreadySubmitted
	s.Title, s.Note = "Retitled", ""
	_, err = e.subs.Create(ctx, e.club.ID, e.round.ID, e.sam.ID, s, t0.Add(time.Hour))
	require.ErrorIs(t, err, ErrAlreadySubmitted)
	
	// Check it was not overwritten
	got2, err := e.subs.ByPlayer(ctx, e.club.ID, e.round.ID, e.sam.ID)
	require.NoError(t, err)
	require.Equal(t, "Title a", got2.Title)
}

func TestSubmissionsDelete(t *testing.T) {
	e := newSubs(t)
	ctx := context.Background()

	got, err := e.subs.Create(ctx, e.club.ID, e.round.ID, e.sam.ID, song("a"), t0)
	require.NoError(t, err)

	// Delete from other club fails
	err = e.subs.Delete(ctx, e.other.ID, e.round.ID, got.ID)
	require.ErrorIs(t, err, ErrNotFound)

	// Delete
	err = e.subs.Delete(ctx, e.club.ID, e.round.ID, got.ID)
	require.NoError(t, err)
	
	// Check it's gone
	_, err = e.subs.ByPlayer(ctx, e.club.ID, e.round.ID, e.sam.ID)
	require.ErrorIs(t, err, ErrNotFound)

	// Submitting again is now allowed
	_, err = e.subs.Create(ctx, e.club.ID, e.round.ID, e.sam.ID, song("b"), t0.Add(time.Hour))
	require.NoError(t, err)
}

func TestSubmissionsCreateRefusals(t *testing.T) {
	e := newSubs(t)
	ctx := context.Background()
	due := *e.round.SubmissionsDueAt

	_, err := e.subs.Create(ctx, e.club.ID, e.round.ID, e.sam.ID, song("a"), due.Add(-time.Second))
	require.NoError(t, err, "just before the deadline")
	for _, at := range []time.Time{due, due.Add(time.Hour)} {
		_, err := e.subs.Create(ctx, e.club.ID, e.round.ID, e.kit.ID, song("b"), at)
		require.ErrorIs(t, err, ErrDeadlinePassed, "at %v", at)
	}

	// Another club's player, an unknown round and another club's ID are all refused.
	bob := e.player(t, e.other, "bob")
	_, err = e.subs.Create(ctx, e.club.ID, e.round.ID, bob.ID, song("c"), t0)
	require.ErrorIs(t, err, ErrNotFound, "player from another club")
	_, err = e.subs.Create(ctx, e.other.ID, e.round.ID, bob.ID, song("c"), t0)
	require.ErrorIs(t, err, ErrNotFound, "round from another club")
	_, err = e.subs.Create(ctx, e.club.ID, 9999, e.sam.ID, song("c"), t0)
	require.ErrorIs(t, err, ErrNotFound, "unknown round")

	// Retired players can't submit.
	require.NoError(t, e.players.SetActive(ctx, e.club.ID, e.kit.ID, false))
	_, err = e.subs.Create(ctx, e.club.ID, e.round.ID, e.kit.ID, song("d"), t0)
	require.ErrorIs(t, err, ErrNotFound, "retired player")

	// Only while submitting.
	for _, phase := range []string{"theme_pending", "voting", "review", "closed"} {
		_, err := e.rounds.db.Exec(`UPDATE rounds SET phase = ? WHERE id = ?`, phase, e.round.ID)
		require.NoError(t, err)
		_, err = e.subs.Create(ctx, e.club.ID, e.round.ID, e.sam.ID, song("e"), t0)
		require.ErrorIs(t, err, ErrWrongPhase, "phase %s", phase)
	}
	got, err := e.subs.ByPlayer(ctx, e.club.ID, e.round.ID, e.sam.ID)
	require.NoError(t, err)
	require.Equal(t, "a", got.SongKey, "refused writes changed the song")
}

func TestSubmissionsReadsAreClubScoped(t *testing.T) {
	e := newSubs(t)
	ctx := context.Background()
	_, err := e.subs.Create(ctx, e.club.ID, e.round.ID, e.sam.ID, song("a"), t0)
	require.NoError(t, err)

	_, err = e.subs.ByPlayer(ctx, e.club.ID, e.round.ID, e.kit.ID)
	require.ErrorIs(t, err, ErrNotFound, "ByPlayer without a song")
	_, err = e.subs.ByPlayer(ctx, e.other.ID, e.round.ID, e.sam.ID)
	require.ErrorIs(t, err, ErrNotFound, "ByPlayer across clubs")

	got, err := e.subs.Submitters(ctx, e.club.ID, e.round.ID)
	require.NoError(t, err)
	require.Equal(t, map[int64]bool{e.sam.ID: true}, got)
	got, err = e.subs.Submitters(ctx, e.other.ID, e.round.ID)
	require.NoError(t, err)
	require.Empty(t, got, "Submitters across clubs")
}
