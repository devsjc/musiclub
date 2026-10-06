package scoring

import (
	"testing"
	"time"
)

func TestScore(t *testing.T) {
	now := time.Now()

	subs := []Submission{
		{ID: 1, PlayerID: 10, SongKey: "song_a", SubmittedAt: now},
		{ID: 2, PlayerID: 20, SongKey: "song_b", SubmittedAt: now.Add(time.Hour)},
		{ID: 3, PlayerID: 30, SongKey: "song_a", SubmittedAt: now.Add(time.Hour * 2)}, // duplicate of 1
	}

	ballots := []Ballot{
		{ID: 100, VoterID: 20, FavoriteSubmissionID: 1, BestFitSubmissionID: 3},
		{ID: 101, VoterID: 30, FavoriteSubmissionID: 1, BestFitSubmissionID: 2},
	}

	res := Results{Submissions: subs, Ballots: ballots}
	rules := Rules{VotePoints: 1, CategoryWinBonus: 1, TrendsetterPerDup: 1}

	events := Score(res, rules)

	var favVotes, bestFitVotes, favWinners, bestFitWinners, trendsetters int
	for _, e := range events {
		switch e.Kind {
		case "favorite_vote":
			favVotes++
		case "best_fit_vote":
			bestFitVotes++
		case "favorite_winner":
			favWinners++
			if e.PlayerID != 10 {
				t.Errorf("expected player 10 to win favorite, got %d", e.PlayerID)
			}
		case "best_fit_winner":
			bestFitWinners++
		case "trendsetter":
			trendsetters++
			if e.PlayerID != 10 {
				t.Errorf("expected player 10 to be trendsetter, got %d", e.PlayerID)
			}
			if *e.SubmissionID != 3 {
				t.Errorf("expected trendsetter event linked to submission 3, got %d", *e.SubmissionID)
			}
		}
	}

	if favVotes != 2 {
		t.Errorf("expected 2 fav votes, got %d", favVotes)
	}
	if bestFitVotes != 2 {
		t.Errorf("expected 2 best fit votes, got %d", bestFitVotes)
	}
	if favWinners != 1 {
		t.Errorf("expected 1 fav winner, got %d", favWinners)
	}
	if bestFitWinners != 2 {
		t.Errorf("expected 2 best fit winners (tie), got %d", bestFitWinners)
	}
	if trendsetters != 1 {
		t.Errorf("expected 1 trendsetter point, got %d", trendsetters)
	}
}
