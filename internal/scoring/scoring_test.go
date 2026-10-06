package scoring

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestScoreSimpleRound(t *testing.T) {
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	subs := []Submission{
		{ID: 1, PlayerID: 10, SongKey: "a", SubmittedAt: t0},
		{ID: 2, PlayerID: 20, SongKey: "b", SubmittedAt: t0.Add(time.Minute)},
	}
	ballots := []Ballot{
		{ID: 100, VoterID: 30, FavoriteSubmissionID: 1, BestFitSubmissionID: 2},
	}

	events := Score(Results{Submissions: subs, Ballots: ballots}, DefaultRules())

	// Expect:
	// - 1 favorite_vote to player 10 (1 pt)
	// - 1 best_fit_vote to player 20 (1 pt)
	// - 1 favorite_winner to player 10 (1 pt)
	// - 1 best_fit_winner to player 20 (1 pt)
	require.Len(t, events, 4)
	require.Equal(t, Event{PlayerID: 10, Kind: "favorite_vote", Points: 1, SubmissionID: ptr(int64(1)), BallotID: ptr(int64(100))}, events[0])
	require.Equal(t, Event{PlayerID: 20, Kind: "best_fit_vote", Points: 1, SubmissionID: ptr(int64(2)), BallotID: ptr(int64(100))}, events[1])
	require.Equal(t, Event{PlayerID: 10, Kind: "favorite_winner", Points: 1, SubmissionID: ptr(int64(1))}, events[2])
	require.Equal(t, Event{PlayerID: 20, Kind: "best_fit_winner", Points: 1, SubmissionID: ptr(int64(2))}, events[3])
}

func TestScoreTiedWinnersShareBonus(t *testing.T) {
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	subs := []Submission{
		{ID: 1, PlayerID: 10, SongKey: "a", SubmittedAt: t0},
		{ID: 2, PlayerID: 20, SongKey: "b", SubmittedAt: t0.Add(time.Minute)},
	}
	ballots := []Ballot{
		{ID: 100, VoterID: 30, FavoriteSubmissionID: 1, BestFitSubmissionID: 1},
		{ID: 101, VoterID: 40, FavoriteSubmissionID: 2, BestFitSubmissionID: 2},
	}

	events := Score(Results{Submissions: subs, Ballots: ballots}, DefaultRules())

	var favWinners, bestWinners []int64
	for _, e := range events {
		if e.Kind == "favorite_winner" {
			favWinners = append(favWinners, e.PlayerID)
		}
		if e.Kind == "best_fit_winner" {
			bestWinners = append(bestWinners, e.PlayerID)
		}
	}
	// Both players tied on favorite (1 vote each) and best-fit (1 vote each)
	require.ElementsMatch(t, []int64{10, 20}, favWinners)
	require.ElementsMatch(t, []int64{10, 20}, bestWinners)
}

func TestScoreZeroVotesNoWinner(t *testing.T) {
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	subs := []Submission{
		{ID: 1, PlayerID: 10, SongKey: "a", SubmittedAt: t0},
	}
	// No ballots cast
	events := Score(Results{Submissions: subs, Ballots: nil}, DefaultRules())
	require.Empty(t, events)
}

func TestScoreTrendsetter(t *testing.T) {
	t0 := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	subs := []Submission{
		{ID: 3, PlayerID: 30, SongKey: "same_song", SubmittedAt: t0.Add(2 * time.Hour)},
		{ID: 1, PlayerID: 10, SongKey: "same_song", SubmittedAt: t0}, // earliest
		{ID: 2, PlayerID: 20, SongKey: "same_song", SubmittedAt: t0.Add(time.Hour)},
		{ID: 4, PlayerID: 40, SongKey: "unique_song", SubmittedAt: t0},
	}

	events := Score(Results{Submissions: subs, Ballots: nil}, DefaultRules())

	// Player 10 gets 2 trendsetter points (1 for ID 2, 1 for ID 3)
	require.Len(t, events, 2)
	require.Equal(t, int64(10), events[0].PlayerID)
	require.Equal(t, "trendsetter", events[0].Kind)
	require.Equal(t, 1, events[0].Points)
	require.Equal(t, int64(2), *events[0].SubmissionID)

	require.Equal(t, int64(10), events[1].PlayerID)
	require.Equal(t, "trendsetter", events[1].Kind)
	require.Equal(t, 1, events[1].Points)
	require.Equal(t, int64(3), *events[1].SubmissionID)
}

func ptr[T any](v T) *T { return &v }
