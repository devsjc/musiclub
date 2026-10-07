package web

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
)

func closedClub(t *testing.T, e *env, slug string) (domain.Club, domain.Player, domain.Player, domain.Player, []domain.Submission) {
	t.Helper()
	c, sam, kit, pat, subs := reviewClub(t, e, slug)

	// Admin features a quote on subs[2]
	quotesURL := "/c/" + slug + "/admin/rounds/2026-03/quotes"
	wantStatus(t, e.do("POST", quotesURL, &sam, c, map[string][]string{
		"submission_id": {fmt.Sprintf("%d", subs[2].ID)},
		"category":      {"best_fit"},
		"quote":         {"Bowie fits rainy moods"},
	}), http.StatusSeeOther)

	// Admin publishes
	publishURL := "/c/" + slug + "/admin/rounds/2026-03/publish"
	wantStatus(t, e.do("POST", publishURL, &sam, c, map[string][]string{
		"confirm_uncurated": {"1"},
	}), http.StatusSeeOther)

	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	require.Equal(t, domain.PhaseClosed, round.Phase)

	return c, sam, kit, pat, subs
}

func TestResultsPage(t *testing.T) {
	e := newEnv(t)
	c, sam, _, _, _ := closedClub(t, e, "friends")

	resultsURL := "/c/friends/rounds/2026-03/results"
	rec := e.do("GET", resultsURL, &sam, c, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	require.Contains(t, body, "Results: 2026-03")
	require.Contains(t, body, "Best Song")
	require.Contains(t, body, "Best Theme Fit")
	require.Contains(t, body, "All Songs")
	require.Contains(t, body, "Points Breakdown")
	require.Contains(t, body, "Guessing Stats")
	require.Contains(t, body, "Bowie fits rainy moods")

	// Round page for closed round redirects to results
	roundURL := "/c/friends/rounds/2026-03"
	recRound := e.do("GET", roundURL, &sam, c, nil)
	wantStatus(t, recRound, http.StatusSeeOther)
	require.Equal(t, resultsURL, recRound.Header().Get("Location"))
}

func TestClubStatsPage(t *testing.T) {
	e := newEnv(t)
	c, sam, _, _, _ := closedClub(t, e, "friends")

	statsURL := "/c/friends/stats"
	rec := e.do("GET", statsURL, &sam, c, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	require.Contains(t, body, "All-Time Stats")
	require.Contains(t, body, "Leaderboard")
	require.Contains(t, body, "Honours")
	require.Contains(t, body, "Guess Matrix")
}

func TestPlayerStatsPage(t *testing.T) {
	e := newEnv(t)
	c, sam, _, _, _ := closedClub(t, e, "friends")

	playerStatsURL := fmt.Sprintf("/c/friends/stats/players/%d", sam.ID)
	rec := e.do("GET", playerStatsURL, &sam, c, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	require.Contains(t, body, "Stats: sam")
	require.Contains(t, body, "Total Points:")
	require.Contains(t, body, "Songs Submitted")
}
