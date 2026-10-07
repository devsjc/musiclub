package web

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
)

func TestRoundPageShowsCountdown(t *testing.T) {
	e := newEnv(t)
	c, sam, _ := submittingClub(t, e, "friends")
	body := e.do("GET", "/c/friends/rounds/2026-03", &sam, c, nil).Body.String()
	require.Contains(t, body, `class="countdown"`)
	require.Contains(t, body, "days left")

	body = e.do("GET", "/c/friends", &sam, c, nil).Body.String()
	require.Contains(t, body, "Submissions are open")
	require.Contains(t, body, "days left")
}

func TestStatusFragmentPolls(t *testing.T) {
	e := newEnv(t)
	c, sam, kit := submittingClub(t, e, "friends")
	wantStatus(t, e.do("POST", "/c/friends/rounds/2026-03/submission", &sam, c, songValues("Queen", "Radio Ga Ga", "https://example.com/x")), http.StatusSeeOther)

	// The round page embeds the poller for the current phase.
	body := e.do("GET", "/c/friends/rounds/2026-03", &kit, c, nil).Body.String()
	require.Contains(t, body, `hx-get="/c/friends/rounds/2026-03/status?phase=submitting"`)
	require.Contains(t, body, `hx-trigger="every 30s"`)

	// The fragment is just the checklist: names, never songs.
	rec := e.do("GET", "/c/friends/rounds/2026-03/status?phase=submitting", &kit, c, nil)
	wantStatus(t, rec, http.StatusOK)
	frag := rec.Body.String()
	require.Contains(t, frag, "Who has submitted")
	require.Contains(t, frag, "✓ sam")
	require.NotContains(t, frag, "✓ kit")
	require.NotContains(t, frag, "<html")
	require.NotContains(t, frag, "Radio Ga Ga")

	// Works for anonymous visitors too (the page does).
	wantStatus(t, e.do("GET", "/c/friends/rounds/2026-03/status?phase=submitting", nil, c, nil), http.StatusOK)
}

func TestStatusFragmentRefreshesPageOnPhaseChange(t *testing.T) {
	e := newEnv(t)
	c, sam, _, _, _ := votingClub(t, e, "friends")

	// A page rendered during submitting is now stale.
	rec := e.do("GET", "/c/friends/rounds/2026-03/status?phase=submitting", &sam, c, nil)
	wantStatus(t, rec, http.StatusNoContent)
	require.Equal(t, "true", rec.Header().Get("HX-Refresh"))

	// A page for the current phase just gets the voters checklist.
	rec = e.do("GET", "/c/friends/rounds/2026-03/status?phase=voting", &sam, c, nil)
	wantStatus(t, rec, http.StatusOK)
	require.Contains(t, rec.Body.String(), "Who has voted")

	// Phases without a checklist always refresh.
	_, err := e.db.Exec(`UPDATE rounds SET phase = 'review'`)
	require.NoError(t, err)
	rec = e.do("GET", "/c/friends/rounds/2026-03/status?phase=review", &sam, c, nil)
	wantStatus(t, rec, http.StatusNoContent)
	require.Equal(t, "true", rec.Header().Get("HX-Refresh"))
}

func TestStatusFragmentIsClubScoped(t *testing.T) {
	e := newEnv(t)
	submittingClub(t, e, "friends")
	other := e.club("other")
	wantStatus(t, e.do("GET", "/c/other/rounds/2026-03/status?phase=submitting", nil, other, nil), http.StatusNotFound)
}

func TestAdminAdvanceRound(t *testing.T) {
	e := newEnv(t)
	c, sam, kit := submittingClub(t, e, "friends")
	advanceURL := "/c/friends/admin/rounds/2026-03/advance"

	// Non-admin cannot advance
	wantStatus(t, e.do("POST", advanceURL, &kit, c, nil), http.StatusForbidden)

	// Admin advances submitting -> voting
	wantStatus(t, e.do("POST", advanceURL, &sam, c, nil), http.StatusSeeOther)
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	require.Equal(t, domain.PhaseVoting, round.Phase)

	// Admin advances voting -> review
	wantStatus(t, e.do("POST", advanceURL, &sam, c, nil), http.StatusSeeOther)
	round, err = e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	require.Equal(t, domain.PhaseReview, round.Phase)

	// Admin advancing during review phase fails
	rec := e.do("POST", advanceURL, &sam, c, nil)
	wantStatus(t, rec, http.StatusConflict)
}

func TestEmptyStates(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")
	sam := e.player(c, "Sam", true)

	// No round: home explains what happens next.
	body := e.do("GET", "/c/friends", &sam, c, nil).Body.String()
	require.Contains(t, body, "No round in progress")
	require.Contains(t, body, "Start one from the admin page")
	body = e.do("GET", "/c/friends", nil, c, nil).Body.String()
	require.Contains(t, body, "An admin will start the next one")

	// No closed rounds: stats say so rather than showing empty tables.
	body = e.do("GET", "/c/friends/stats", &sam, c, nil).Body.String()
	require.Contains(t, body, "No rounds have been published yet")
	require.NotContains(t, body, "Guess Matrix")

	// Admin with a round but no submissions.
	wantStatus(t, e.do("POST", "/c/friends/admin/rounds", &sam, c, url.Values{"month": {"2026-03"}}), http.StatusSeeOther)
	wantStatus(t, e.do("POST", "/c/friends/rounds/2026-03/theme", &sam, c, url.Values{"theme": {"Rain"}}), http.StatusSeeOther)
	body = e.do("GET", "/c/friends/admin", &sam, c, nil).Body.String()
	require.Contains(t, body, "Nobody has submitted a song yet")

	// Voting with no songs: nothing to vote on, and no broken ballot form.
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	_, err = e.db.Exec(`UPDATE rounds SET phase = 'voting', voting_due_at = datetime('now', '+7 days') WHERE id = ?`, round.ID)
	require.NoError(t, err)
	body = e.do("GET", "/c/friends/rounds/2026-03", &sam, c, nil).Body.String()
	require.Contains(t, body, "nothing to vote on")
	require.NotContains(t, body, "Submit ballot")
}
