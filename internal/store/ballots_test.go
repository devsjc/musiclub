package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
)

func newVotingRound(t *testing.T) (*subsEnv, *Ballots, domain.Submission, domain.Submission) {
	t.Helper()
	e := newSubs(t)
	ctx := context.Background()

	sub1, err := e.subs.Create(ctx, e.club.ID, e.round.ID, e.sam.ID, song("1"), t0)
	require.NoError(t, err)
	sub2, err := e.subs.Create(ctx, e.club.ID, e.round.ID, e.kit.ID, song("2"), t0)
	require.NoError(t, err)

	voteDue := t0.AddDate(0, 0, 14)
	_, err = e.rounds.db.Exec(`UPDATE rounds SET phase = 'voting', voting_due_at = ? WHERE id = ?`,
		voteDue.UTC().Format(sqliteTime), e.round.ID)
	require.NoError(t, err)

	ballots := NewBallots(e.rounds.db)
	return e, ballots, sub1, sub2
}

func TestBallotsCreateAndByVoter(t *testing.T) {
	e, ballots, sub1, sub2 := newVotingRound(t)
	ctx := context.Background()

	b := domain.Ballot{
		RoundID:              e.round.ID,
		VoterID:              e.sam.ID,
		FavoriteSubmissionID: sub2.ID,
		FavoriteComment:      "Awesome",
		BestFitSubmissionID:  sub1.ID,
		BestFitComment:       "Fits the theme",
		Guesses: []domain.Guess{
			{SubmissionID: sub2.ID, GuessedPlayerID: e.kit.ID},
		},
	}

	require.NoError(t, ballots.Create(ctx, e.club.ID, b, t0))

	// Second vote returns ErrAlreadyVoted
	err := ballots.Create(ctx, e.club.ID, b, t0.Add(time.Hour))
	require.ErrorIs(t, err, ErrAlreadyVoted)

	// ByVoter
	got, err := ballots.ByVoter(ctx, e.club.ID, e.round.ID, e.sam.ID)
	require.NoError(t, err)
	require.Equal(t, e.sam.ID, got.VoterID)
	require.Equal(t, sub2.ID, got.FavoriteSubmissionID)
	require.Equal(t, "Awesome", got.FavoriteComment)
	require.Equal(t, sub1.ID, got.BestFitSubmissionID)
	require.Equal(t, "Fits the theme", got.BestFitComment)
	require.Len(t, got.Guesses, 1)
	require.Equal(t, sub2.ID, got.Guesses[0].SubmissionID)
	require.Equal(t, e.kit.ID, got.Guesses[0].GuessedPlayerID)

	// Voters set
	voters, err := ballots.Voters(ctx, e.club.ID, e.round.ID)
	require.NoError(t, err)
	require.True(t, voters[e.sam.ID])
	require.False(t, voters[e.kit.ID])
}

func TestBallotsCreateRefusals(t *testing.T) {
	e, ballots, sub1, sub2 := newVotingRound(t)
	ctx := context.Background()

	b := domain.Ballot{
		RoundID:              e.round.ID,
		VoterID:              e.sam.ID,
		FavoriteSubmissionID: sub2.ID,
		BestFitSubmissionID:  sub1.ID,
	}

	// Inactive player cannot vote
	require.NoError(t, e.players.SetActive(ctx, e.club.ID, e.sam.ID, false))
	require.ErrorIs(t, ballots.Create(ctx, e.club.ID, b, t0), ErrNotFound)
	require.NoError(t, e.players.SetActive(ctx, e.club.ID, e.sam.ID, true))

	// Deadline passed
	past := t0.AddDate(0, 0, 15)
	require.ErrorIs(t, ballots.Create(ctx, e.club.ID, b, past), ErrDeadlinePassed)

	// Wrong phase
	_, err := e.rounds.db.Exec(`UPDATE rounds SET phase = 'submitting' WHERE id = ?`, e.round.ID)
	require.NoError(t, err)
	require.ErrorIs(t, ballots.Create(ctx, e.club.ID, b, t0), ErrWrongPhase)
}
