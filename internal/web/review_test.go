package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
)

func reviewClub(t *testing.T, e *env, slug string) (domain.Club, domain.Player, domain.Player, domain.Player, []domain.Submission) {
	t.Helper()
	c, sam, kit, pat, subs := votingClub(t, e, slug)

	// Cast votes
	page := "/c/" + slug + "/rounds/2026-03"
	formSam := url.Values{
		"favorite_submission_id": {fmt.Sprintf("%d", subs[1].ID)},
		"favorite_comment":       {"Best song ever"},
		"best_fit_submission_id": {fmt.Sprintf("%d", subs[2].ID)},
		"best_fit_comment":       {"Perfect theme fit"},
		fmt.Sprintf("guess_%d", subs[1].ID): {fmt.Sprintf("%d", kit.ID)},
		fmt.Sprintf("guess_%d", subs[2].ID): {fmt.Sprintf("%d", pat.ID)},
	}
	wantStatus(t, e.do("POST", page+"/ballot", &sam, c, formSam), http.StatusSeeOther)

	formKit := url.Values{
		"favorite_submission_id": {fmt.Sprintf("%d", subs[0].ID)},
		"favorite_comment":       {"Kit likes Sam's song"},
		"best_fit_submission_id": {fmt.Sprintf("%d", subs[2].ID)},
		"best_fit_comment":       {"Kit also agrees on song 3"},
		fmt.Sprintf("guess_%d", subs[0].ID): {fmt.Sprintf("%d", sam.ID)},
		fmt.Sprintf("guess_%d", subs[2].ID): {fmt.Sprintf("%d", pat.ID)},
	}
	wantStatus(t, e.do("POST", page+"/ballot", &kit, c, formKit), http.StatusSeeOther)

	// Move round to review phase
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE rounds SET phase = 'review' WHERE id = ?`, round.ID)
	require.NoError(t, err)

	return c, sam, kit, pat, subs
}

func TestReviewPageRendersAndInteractions(t *testing.T) {
	e := newEnv(t)
	c, sam, kit, _, subs := reviewClub(t, e, "friends")
	reviewURL := "/c/friends/admin/rounds/2026-03/review"

	// Non-admin is forbidden
	wantStatus(t, e.do("GET", reviewURL, &kit, c, nil), http.StatusForbidden)

	// Admin views review page
	rec := e.do("GET", reviewURL, &sam, c, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	require.Contains(t, body, "Review 2026-03")
	require.Contains(t, body, "Category Winners & Votes")
	require.Contains(t, body, "Best song ever")
	require.Contains(t, body, "Song Keys")
	require.Contains(t, body, "Publish")

	// Admin updates song key
	keyURL := "/c/friends/admin/rounds/2026-03/song-keys"
	wantStatus(t, e.do("POST", keyURL, &sam, c, url.Values{
		"submission_id": {fmt.Sprintf("%d", subs[0].ID)},
		"song_key":      {"queen-edited-key"},
	}), http.StatusSeeOther)

	sub, err := e.submissions.ByPlayer(context.Background(), c.ID, subs[0].RoundID, subs[0].PlayerID)
	require.NoError(t, err)
	require.Equal(t, "queen-edited-key", sub.SongKey)

	// Admin features a quote
	quotesURL := "/c/friends/admin/rounds/2026-03/quotes"
	wantStatus(t, e.do("POST", quotesURL, &sam, c, url.Values{
		"submission_id": {fmt.Sprintf("%d", subs[1].ID)},
		"category":      {"favorite"},
		"quote":         {"Featured comment!"},
	}), http.StatusSeeOther)

	body = e.do("GET", reviewURL, &sam, c, nil).Body.String()
	require.Contains(t, body, "Featured comment!")

	// Admin publishes
	publishURL := "/c/friends/admin/rounds/2026-03/publish"
	recPub := e.do("POST", publishURL, &sam, c, nil)
	wantStatus(t, recPub, http.StatusSeeOther)
	require.Equal(t, "/c/friends/rounds/2026-03/results", recPub.Header().Get("Location"))

	// Round is now closed
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	require.Equal(t, domain.PhaseClosed, round.Phase)

	// Admin recomputes
	recompURL := "/c/friends/admin/rounds/2026-03/recompute"
	wantStatus(t, e.do("POST", recompURL, &sam, c, nil), http.StatusSeeOther)
}
