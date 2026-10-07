package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
)

func votingClub(t *testing.T, e *env, slug string) (domain.Club, domain.Player, domain.Player, domain.Player, []domain.Submission) {
	t.Helper()
	c, sam, kit := submittingClub(t, e, slug)
	pat := e.player(c, "Pat", false)

	// All 3 players submit
	page := "/c/" + slug + "/rounds/2026-03"
	wantStatus(t, e.do("POST", page+"/submission", &sam, c, songValues("Queen", "Song 1", "https://example.com/1")), http.StatusSeeOther)
	wantStatus(t, e.do("POST", page+"/submission", &kit, c, songValues("Beatles", "Song 2", "https://example.com/2")), http.StatusSeeOther)
	wantStatus(t, e.do("POST", page+"/submission", &pat, c, songValues("Bowie", "Song 3", "https://example.com/3")), http.StatusSeeOther)

	// Advance to voting
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)

	voteDue := time.Now().AddDate(0, 0, 14).UTC().Format("2006-01-02 15:04:05")
	_, err = e.db.Exec(`UPDATE rounds SET phase = 'voting', voting_due_at = ? WHERE id = ?`, voteDue, round.ID)
	require.NoError(t, err)

	subs, err := e.submissions.List(context.Background(), c.ID, round.ID)
	require.NoError(t, err)

	return c, sam, kit, pat, subs
}

func TestVotingPageRenders(t *testing.T) {
	e := newEnv(t)
	c, sam, _, _, subs := votingClub(t, e, "friends")
	page := "/c/friends/rounds/2026-03"

	body := e.do("GET", page, &sam, c, nil).Body.String()

	// Songs are shown with Listen link
	require.Contains(t, body, "Queen — Song 1")
	require.Contains(t, body, "Beatles — Song 2")
	require.Contains(t, body, "Bowie — Song 3")
	require.Contains(t, body, `rel="noopener noreferrer nofollow"`)

	// Ballot form is present
	require.Contains(t, body, "Favorite song")
	require.Contains(t, body, "Best fit for theme")
	require.Contains(t, body, "Guesses")
	require.Contains(t, body, "Submit ballot")

	// Voter's own song is excluded from favorite/best fit options
	require.NotContains(t, body, fmt.Sprintf(`<option value="%d">Queen — Song 1</option>`, subs[0].ID))
	require.Contains(t, body, fmt.Sprintf(`<option value="%d">Beatles — Song 2</option>`, subs[1].ID))

	// Who has voted checklist
	require.Contains(t, body, "Who has voted")
}

func TestBallotGuessesHTMXFragment(t *testing.T) {
	e := newEnv(t)
	c, sam, kit, pat, subs := votingClub(t, e, "friends")
	page := "/c/friends/rounds/2026-03/ballot-guesses"

	// Request fragment with guess for subs[1] set to kit
	rec := e.do("GET", page+"?"+url.Values{
		fmt.Sprintf("guess_%d", subs[1].ID): {fmt.Sprintf("%d", kit.ID)},
	}.Encode(), &sam, c, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()

	// Fieldset is present
	require.Contains(t, body, `id="guesses-fieldset"`)
	// For subs[1], kit is selected
	require.Contains(t, body, fmt.Sprintf(`<option value="%d" selected>kit</option>`, kit.ID))
	// For subs[2], kit is disabled because already selected
	require.Contains(t, body, fmt.Sprintf(`<option value="%d" disabled>kit</option>`, kit.ID))
	// For subs[2], pat is available
	require.Contains(t, body, fmt.Sprintf(`<option value="%d">pat</option>`, pat.ID))
}

func TestBallotSubmissionFlow(t *testing.T) {
	e := newEnv(t)
	c, sam, kit, pat, subs := votingClub(t, e, "friends")
	page := "/c/friends/rounds/2026-03"

	ballotForm := url.Values{
		"favorite_submission_id":            {fmt.Sprintf("%d", subs[1].ID)},
		"favorite_comment":                  {"Love this song"},
		"best_fit_submission_id":            {fmt.Sprintf("%d", subs[2].ID)},
		"best_fit_comment":                  {"Fits the rainy theme"},
		fmt.Sprintf("guess_%d", subs[1].ID): {fmt.Sprintf("%d", kit.ID)},
		fmt.Sprintf("guess_%d", subs[2].ID): {fmt.Sprintf("%d", pat.ID)},
	}

	// Submit ballot
	rec := e.do("POST", page+"/ballot", &sam, c, ballotForm)
	wantStatus(t, rec, http.StatusSeeOther)
	require.Equal(t, page, rec.Header().Get("Location"))

	// Reload page: ballot form is gone, "You have cast your ballot" is shown
	body := e.do("GET", page, &sam, c, nil).Body.String()
	require.Contains(t, body, "You have cast your ballot.")
	require.NotContains(t, body, "Submit ballot")

	// Checklist ticks Sam
	require.Contains(t, body, "✓ sam")
	require.NotContains(t, body, "✓ kit")

	// Submitting a second ballot is rejected
	rec2 := e.do("POST", page+"/ballot", &sam, c, ballotForm)
	wantStatus(t, rec2, http.StatusConflict)

	// Invalid ballot is rejected
	invalidForm := url.Values{
		"favorite_submission_id":            {fmt.Sprintf("%d", subs[1].ID)},
		"best_fit_submission_id":            {fmt.Sprintf("%d", subs[1].ID)}, // same!
		fmt.Sprintf("guess_%d", subs[1].ID): {fmt.Sprintf("%d", kit.ID)},
		fmt.Sprintf("guess_%d", subs[2].ID): {fmt.Sprintf("%d", pat.ID)},
	}
	rec3 := e.do("POST", page+"/ballot", &kit, c, invalidForm)
	require.Equal(t, http.StatusUnprocessableEntity, rec3.Code)
}
