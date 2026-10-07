package rounds

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
	"musiclub/internal/store"
)

func TestTickAdvancesSubmittingToVoting(t *testing.T) {
	s, club, sam, _ := newService(t)
	ctx := context.Background()
	r, err := s.StartRound(ctx, club.ID, "2026-03")
	require.NoError(t, err)

	require.NoError(t, s.SetTheme(ctx, club.ID, r.ID, r.PickerID, "Rain"))

	// Time passes, but deadline not met and player hasn't submitted
	require.NoError(t, s.Tick(ctx, s.now()))
	r, err = s.rounds.Get(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PhaseSubmitting, r.Phase)

	// Submitting
	_, err = s.SubmitSong(ctx, club.ID, r.ID, sam.ID, Song{
		Title: "Rain", Artist: "The Beatles", URL: "https://example.com/rain",
	})
	require.NoError(t, err)

	// Since sam is the only player and has submitted, Tick should advance to voting
	require.NoError(t, s.Tick(ctx, s.now()))
	r, err = s.rounds.Get(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PhaseVoting, r.Phase)
	require.NotNil(t, r.VotingDueAt)
	wantDue := s.now().AddDate(0, 0, 14) // default voting days is 14
	require.True(t, r.VotingDueAt.Equal(wantDue))

	// Submissions should have assigned positions
	sub, err := s.subs.ByPlayer(ctx, club.ID, r.ID, sam.ID)
	require.NoError(t, err)
	require.NotNil(t, sub.Position)
	require.Equal(t, 1, *sub.Position)
}

func TestTickAdvancesSubmittingByDeadline(t *testing.T) {
	s, club, _, database := newService(t)
	ctx := context.Background()

	// Add second player who won't submit
	_, err := store.NewPlayers(database).Create(ctx, club.ID, "alex", false)
	require.NoError(t, err)

	r, err := s.StartRound(ctx, club.ID, "2026-03")
	require.NoError(t, err)
	require.NoError(t, s.SetTheme(ctx, club.ID, r.ID, r.PickerID, "Rain"))

	// Submitting deadline is in 14 days. Advance time 15 days.
	future := s.now().AddDate(0, 0, 15)
	require.NoError(t, s.Tick(ctx, future))

	r, err = s.rounds.Get(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PhaseVoting, r.Phase)
}

func TestTickAdvancesVotingToReviewByDeadline(t *testing.T) {
	s, club, _, _ := newService(t)
	ctx := context.Background()

	r, err := s.StartRound(ctx, club.ID, "2026-03")
	require.NoError(t, err)
	require.NoError(t, s.SetTheme(ctx, club.ID, r.ID, r.PickerID, "Rain"))

	// Submitting deadline is in 14 days. Advance time 15 days.
	future := s.now().AddDate(0, 0, 15)
	require.NoError(t, s.Tick(ctx, future))

	r, err = s.rounds.Get(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PhaseVoting, r.Phase)

	// Voting deadline is in another 14 days. Advance time 30 days total.
	futureVoting := s.now().AddDate(0, 0, 30)
	require.NoError(t, s.Tick(ctx, futureVoting))

	r, err = s.rounds.Get(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PhaseReview, r.Phase)
}
