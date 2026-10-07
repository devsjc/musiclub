package web

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
	"musiclub/internal/linkresolver"
)

// submittingClub sets up a club with players sam (admin) and kit, and a round for
// 2026-03 whose theme has been set by whichever of them was chosen as picker.
func submittingClub(t *testing.T, e *env, slug string) (c domain.Club, sam, kit domain.Player) {
	t.Helper()
	c = e.club(slug)
	sam, kit = e.player(c, "Sam", true), e.player(c, "Kit", false)
	wantStatus(t, e.do("POST", "/c/"+slug+"/admin/rounds", &sam, c, url.Values{"month": {"2026-03"}}), http.StatusSeeOther)
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	picker := sam
	if round.PickerID == kit.ID {
		picker = kit
	}
	wantStatus(t, e.do("POST", "/c/"+slug+"/rounds/2026-03/theme", &picker, c, url.Values{"theme": {"Rain"}}), http.StatusSeeOther)
	return c, sam, kit
}

func songValues(artist, title, link string) url.Values {
	return url.Values{"artist": {artist}, "title": {title}, "url": {link}, "note": {""}}
}

func TestSubmissionFlow(t *testing.T) {
	e := newEnv(t)
	c, sam, kit := submittingClub(t, e, "friends")
	page := "/c/friends/rounds/2026-03"
	post := func(as *domain.Player, f url.Values) *httptest.ResponseRecorder {
		return e.do("POST", page+"/submission", as, c, f)
	}

	// Anonymous visitors are told to say who they are, and can't submit.
	body := e.do("GET", page, nil, c, nil).Body.String()
	require.NotContains(t, body, `name="artist"`, "anonymous visitor should not see the form")
	require.Contains(t, body, "Tell us who you are")
	rec := post(nil, songValues("Queen", "Radio Ga Ga", "https://example.com/x"))
	wantStatus(t, rec, http.StatusSeeOther)
	require.Equal(t, "/c/friends/whoami", rec.Header().Get("Location"), "anonymous submit")

	// Players see the form and a checklist with nobody ticked.
	body = e.do("GET", page, &sam, c, nil).Body.String()
	require.Contains(t, body, `name="artist"`)
	require.Contains(t, body, "Find song")
	require.NotContains(t, body, "✓")

	// A good submission is saved and ticks the submitter off, by name only.
	wantStatus(t, post(&sam, songValues("Queen", "Radio Ga Ga", "https://open.spotify.com/track/abc?si=x")), http.StatusSeeOther)
	body = e.do("GET", page, &sam, c, nil).Body.String()
	require.NotContains(t, body, `name="artist"`, "form is hidden after submitting")
	require.Contains(t, body, "Queen — Radio Ga Ga")
	require.Contains(t, body, "✓ sam")

	other := e.do("GET", page, &kit, c, nil).Body.String()
	require.Contains(t, other, "✓ sam")
	require.NotContains(t, other, "✓ kit", "checklist should tick only sam")
	for _, secret := range []string{"Queen", "Radio Ga Ga", "spotify"} {
		require.NotContains(t, other, secret, "kit can see sam's song before the reveal")
	}

	// Submitting again fails.
	rec = post(&sam, songValues("Queen", "Under Pressure", "https://example.com/y"))
	wantStatus(t, rec, http.StatusConflict)
	require.Contains(t, rec.Body.String(), "You have already submitted")
}

// fakeProvider returns canned suggestions and counts how often it is asked.
type fakeProvider struct {
	matches []linkresolver.Match
	calls   int
}

func (f *fakeProvider) Search(context.Context, string, string) ([]linkresolver.Match, error) {
	f.calls++
	return f.matches, nil
}

var hiddenInput = regexp.MustCompile(`<input type="hidden" name="([^"]+)" value="([^"]*)">`)

// hiddenFields is what a browser would post back from the fragment's hidden inputs.
func hiddenFields(body string) url.Values {
	out := url.Values{}
	for _, m := range hiddenInput.FindAllStringSubmatch(body, -1) {
		out.Add(m[1], html.UnescapeString(m[2]))
	}
	return out
}

func savedSong(t *testing.T, e *env, c domain.Club, p domain.Player) domain.Submission {
	t.Helper()
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	sub, err := e.submissions.ByPlayer(context.Background(), c.ID, round.ID, p.ID)
	require.NoError(t, err)
	return sub
}

func TestSubmissionSuggestionsFlow(t *testing.T) {
	provider := &fakeProvider{matches: []linkresolver.Match{
		{ExternalID: "vid1", URL: linkresolver.MatchURLPrefix + "vid1", Title: "Radio Ga Ga (Live)", Artist: "Queen"},
		{ExternalID: "vid2", URL: linkresolver.MatchURLPrefix + "vid2", Title: "Radio Ga Ga", Artist: "Queen"},
	}}
	e := newEnvWith(t, provider)
	c, sam, _ := submittingClub(t, e, "friends")
	page := "/c/friends/rounds/2026-03"
	typed := url.Values{"artist": {"queen"}, "title": {"radio gaga"}, "note": {"Cool note"}}

	// Step 1: the search offers each suggestion and lets the player paste their own instead.
	rec := e.do("POST", page+"/submission/search", &sam, c, typed)
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	require.Contains(t, body, "Queen — Radio Ga Ga (Live)")
	require.Contains(t, body, "Queen — Radio Ga Ga")
	require.Contains(t, body, "None of these")
	require.Contains(t, body, `#submission-form [name='artist']`, "the paste-my-own step needs the typed artist and title")

	// Searching again is served from the cache.
	wantStatus(t, e.do("POST", page+"/submission/search", &sam, c, typed), http.StatusOK)
	require.Equal(t, 1, provider.calls, "provider calls")

	// Step 2: confirming the second suggestion stores the service's own artist and title.
	form := hiddenFields(body)
	form.Set("candidate_index", "1")
	wantStatus(t, e.do("POST", page+"/submission", &sam, c, form), http.StatusSeeOther)
	sub := savedSong(t, e, c, sam)
	require.Equal(t, "Queen", sub.Artist)
	require.Equal(t, "Radio Ga Ga", sub.Title)
	require.Equal(t, linkresolver.MatchURLPrefix+"vid2", sub.URL)
	require.Equal(t, "youtube_music", sub.URLService)
	require.Equal(t, "matched", sub.LinkSource)
	require.Equal(t, "vid2", sub.ExternalID)
	require.Equal(t, "Cool note", sub.Note)
}

func TestSubmissionWithoutSuggestionsAsksForALink(t *testing.T) {
	e := newEnv(t) // no provider configured
	c, sam, _ := submittingClub(t, e, "friends")
	page := "/c/friends/rounds/2026-03"

	rec := e.do("POST", page+"/submission/search", &sam, c, url.Values{"artist": {"Queen"}, "title": {"Radio Ga Ga"}, "note": {""}})
	wantStatus(t, rec, http.StatusOK)
	body := rec.Body.String()
	require.Contains(t, body, "Couldn't find matches")
	require.Equal(t, 1, strings.Count(body, `name="link_source"`), "a pasted link must not also claim to be a suggestion")

	form := hiddenFields(body)
	form.Set("url", "https://example.com/radio")
	wantStatus(t, e.do("POST", page+"/submission", &sam, c, form), http.StatusSeeOther)
	sub := savedSong(t, e, c, sam)
	require.Equal(t, "manual", sub.LinkSource)
	require.Empty(t, sub.ExternalID)
	require.Equal(t, "Queen", sub.Artist)
	require.Equal(t, "other", sub.URLService)

	// The "paste my own link" fragment carries the typed song through.
	rec = e.do("GET", page+"/submission/manual-url?"+url.Values{"artist": {"A"}, "title": {"T"}, "note": {"n"}}.Encode(), &sam, c, nil)
	wantStatus(t, rec, http.StatusOK)
	require.Equal(t, url.Values{"artist": {"A"}, "title": {"T"}, "note": {"n"}, "link_source": {"manual"}}, hiddenFields(rec.Body.String()))
}

func TestSubmissionRefusesForeignSuggestionLinks(t *testing.T) {
	e := newEnv(t)
	c, sam, _ := submittingClub(t, e, "friends")
	post := func(link string) *httptest.ResponseRecorder {
		return e.do("POST", "/c/friends/rounds/2026-03/submission", &sam, c, url.Values{
			"artist": {"Queen"}, "title": {"Radio Ga Ga"}, "link_source": {"matched"}, "candidate_index": {"0"},
			"match_0_url": {link}, "match_0_extid": {"x"},
		})
	}
	// Suggestions round-trip through the browser, so only our own links count.
	for _, link := range []string{"https://evil.example/watch?v=x", "https://www.youtube.com/watch?v=x", ""} {
		rec := post(link)
		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, link)
	}
	require.NotContains(t, e.do("GET", "/c/friends/rounds/2026-03", &sam, c, nil).Body.String(), "✓", "nothing saved")
	wantStatus(t, post(linkresolver.MatchURLPrefix+"x"), http.StatusSeeOther)
}

func TestAdminDeleteSubmission(t *testing.T) {
	e := newEnv(t)
	c, sam, kit := submittingClub(t, e, "friends")
	page := "/c/friends/rounds/2026-03"

	// kit submits
	wantStatus(t, e.do("POST", page+"/submission", &kit, c, songValues("Queen", "Radio Ga Ga", "https://example.com/x")), http.StatusSeeOther)

	// find the submission ID
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	sub, err := e.submissions.ByPlayer(context.Background(), c.ID, round.ID, kit.ID)
	require.NoError(t, err)

	delURL := "/c/friends/admin/rounds/2026-03/submissions/" + strconv.FormatInt(sub.ID, 10) + "/delete"

	// kit cannot delete (not admin)
	wantStatus(t, e.do("POST", delURL, &kit, c, nil), http.StatusForbidden)

	// sam (admin) deletes
	wantStatus(t, e.do("POST", delURL, &sam, c, nil), http.StatusSeeOther)

	// kit can submit again
	wantStatus(t, e.do("POST", page+"/submission", &kit, c, songValues("David Bowie", "Heroes", "https://example.com/y")), http.StatusSeeOther)
}

func TestSubmissionRejectsBadInputAndKeepsWhatWasTyped(t *testing.T) {
	e := newEnv(t)
	c, sam, _ := submittingClub(t, e, "friends")
	post := func(f url.Values) *httptest.ResponseRecorder {
		return e.do("POST", "/c/friends/rounds/2026-03/submission", &sam, c, f)
	}

	for name, f := range map[string]url.Values{
		"no artist":   songValues("", "Title", "https://example.com/x"),
		"no title":    songValues("Artist", " ", "https://example.com/x"),
		"no link":     songValues("Artist", "Title", ""),
		"script link": songValues("Artist", "Title", "javascript:alert(1)"),
		"ftp link":    songValues("Artist", "Title", "ftp://example.com/x"),
		"long note":   {"artist": {"A"}, "title": {"T"}, "url": {"https://example.com/x"}, "note": {strings.Repeat("x", 501)}},
		"punctuation": songValues("Artist", "!!!", "https://example.com/x"),
	} {
		require.Equal(t, http.StatusUnprocessableEntity, post(f).Code, name)
	}

	rec := post(songValues("Some <b>Artist</b>", "Title", "not a url"))
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	body := rec.Body.String()
	require.Contains(t, body, "full http")
	require.Contains(t, body, `value="Some &lt;b&gt;Artist&lt;/b&gt;"`, "input should be kept, escaped")
	require.Contains(t, body, `value="not a url"`)
	require.NotContains(t, e.do("GET", "/c/friends/rounds/2026-03", &sam, c, nil).Body.String(), "✓", "rejected submissions must not be saved")
}

func TestSubmissionRejectedOutsideSubmittingPhase(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")
	admin := e.player(c, "Sam", true)
	wantStatus(t, e.do("POST", "/c/friends/admin/rounds", &admin, c, url.Values{"month": {"2026-03"}}), http.StatusSeeOther)
	post := func() *httptest.ResponseRecorder {
		return e.do("POST", "/c/friends/rounds/2026-03/submission", &admin, c, songValues("A", "T", "https://example.com/x"))
	}

	// Theme not set yet.
	wantStatus(t, post(), http.StatusConflict)
	wantStatus(t, e.do("POST", "/c/friends/rounds/2026-04/submission", &admin, c, songValues("A", "T", "https://example.com/x")), 404)

	wantStatus(t, e.do("POST", "/c/friends/rounds/2026-03/theme", &admin, c, url.Values{"theme": {"Rain"}}), http.StatusSeeOther)
	wantStatus(t, post(), http.StatusSeeOther)

	// Later phases and a passed deadline both refuse writes.
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	past := time.Now().Add(-time.Hour).UTC().Format("2006-01-02 15:04:05")
	_, err = e.db.Exec(`UPDATE rounds SET submissions_due_at = ? WHERE id = ?`, past, round.ID)
	require.NoError(t, err)
	wantStatus(t, post(), http.StatusConflict)
	body := e.do("GET", "/c/friends/rounds/2026-03", &admin, c, nil).Body.String()
	require.NotContains(t, body, `name="artist"`, "form should be gone after the deadline")

	_, err = e.db.Exec(`UPDATE rounds SET phase = 'voting' WHERE id = ?`, round.ID)
	require.NoError(t, err)
	wantStatus(t, post(), http.StatusConflict)
}

func TestSubmissionsAreClubScoped(t *testing.T) {
	e := newEnv(t)
	a, sam, _ := submittingClub(t, e, "a")
	b, bob, _ := submittingClub(t, e, "b")

	wantStatus(t, e.do("POST", "/c/a/rounds/2026-03/submission", &sam, a, songValues("Queen", "Radio Ga Ga", "https://example.com/x")), http.StatusSeeOther)

	// Club b's page for the same month shows nothing of club a's, and its checklist is its own.
	body := e.do("GET", "/c/b/rounds/2026-03", &bob, b, nil).Body.String()
	require.NotContains(t, body, "✓", "club b sees club a's submission")
	require.NotContains(t, body, "Queen", "club b sees club a's submission")
	// A cookie for club a doesn't let anyone submit in club b.
	req := e.do("POST", "/c/b/rounds/2026-03/submission", &sam, a, songValues("X", "Y", "https://example.com/x"))
	wantStatus(t, req, http.StatusSeeOther)
	require.Equal(t, "/c/b/whoami", req.Header().Get("Location"))
}
