package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/db"
	"musiclub/internal/domain"
	"musiclub/internal/store"
)

type env struct {
	t           *testing.T
	db          *sql.DB
	h           http.Handler
	clubs       *store.Clubs
	players     *store.Players
	rounds      *store.Rounds
	submissions *store.Submissions
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return &env{
		t:           t,
		db:          d,
		h:           NewServer(d, "test-secret", time.UTC).Handler(),
		clubs:       store.NewClubs(d),
		players:     store.NewPlayers(d),
		rounds:      store.NewRounds(d),
		submissions: store.NewSubmissions(d),
	}
}

func (e *env) club(slug string) domain.Club {
	e.t.Helper()
	c, err := e.clubs.Create(context.Background(), slug, slug+" Club")
	require.NoError(e.t, err)
	return c
}

func (e *env) player(c domain.Club, name string, admin bool) domain.Player {
	e.t.Helper()
	p, err := e.players.Create(context.Background(), c.ID, name, admin)
	require.NoError(e.t, err)
	return p
}

// do sends a request, optionally as a player of the club (via the real whoami flow).
func (e *env) do(method, path string, as *domain.Player, club domain.Club, form url.Values) *httptest.ResponseRecorder {
	e.t.Helper()
	var req *http.Request
	if form != nil {
		req = httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if as != nil {
		req.AddCookie(e.login(club, as))
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) login(c domain.Club, p *domain.Player) *http.Cookie {
	e.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/c/"+c.Slug+"/whoami",
		strings.NewReader(url.Values{"player_id": {strconv.FormatInt(p.ID, 10)}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	e.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == cookieName(c.Slug) {
			return ck
		}
	}
	require.FailNow(e.t, "whoami set no cookie", "status %d", rec.Code)
	return nil
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	require.Equal(t, want, rec.Code, "body:\n%s", rec.Body)
}

func TestIndex(t *testing.T) {
	e := newEnv(t)

	rec := e.do("GET", "/", nil, domain.Club{}, nil)
	wantStatus(t, rec, 200)
	require.Contains(t, rec.Body.String(), "No clubs yet")

	e.club("one")
	rec = e.do("GET", "/", nil, domain.Club{}, nil)
	wantStatus(t, rec, http.StatusFound)
	require.Equal(t, "/c/one", rec.Header().Get("Location"))

	e.club("two")
	rec = e.do("GET", "/", nil, domain.Club{}, nil)
	wantStatus(t, rec, 200)
	require.Contains(t, rec.Body.String(), "/c/one")
	require.Contains(t, rec.Body.String(), "/c/two")
}

func TestClubMiddleware(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")

	wantStatus(t, e.do("GET", "/c/nope", nil, c, nil), 404)
	wantStatus(t, e.do("GET", "/c/nope/whoami", nil, c, nil), 404)

	rec := e.do("GET", "/c/friends", nil, c, nil)
	wantStatus(t, rec, 200)
	require.Contains(t, rec.Body.String(), "friends club")
}

func TestWhoami(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")
	sam := e.player(c, "Sam", false)
	e.player(c, "Kit", true)
	retired := e.player(c, "Retired", false)
	require.NoError(t, e.players.SetActive(context.Background(), c.ID, retired.ID, false))

	rec := e.do("GET", "/c/friends/whoami", nil, c, nil)
	wantStatus(t, rec, 200)
	body := rec.Body.String()
	require.Contains(t, body, "sam")
	require.Contains(t, body, "kit")
	require.NotContains(t, body, "retired", "whoami should list active players only")

	// Picking a player sets a scoped, HttpOnly cookie and the home page greets them.
	rec = e.do("POST", "/c/friends/whoami", nil, c, url.Values{"player_id": {strconv.FormatInt(sam.ID, 10)}})
	wantStatus(t, rec, http.StatusSeeOther)
	ck := rec.Result().Cookies()[0]
	require.Equal(t, "mc_friends", ck.Name)
	require.Equal(t, "/c/friends", ck.Path)
	require.True(t, ck.HttpOnly)
	rec = e.do("GET", "/c/friends", &sam, c, nil)
	require.Contains(t, rec.Body.String(), "Hi sam.")

	// Retired, unknown and non-numeric players are rejected.
	for _, id := range []string{strconv.FormatInt(retired.ID, 10), "9999", "x", ""} {
		rec = e.do("POST", "/c/friends/whoami", nil, c, url.Values{"player_id": {id}})
		wantStatus(t, rec, http.StatusBadRequest)
		require.Empty(t, rec.Result().Cookies(), "player_id %q: cookie set on rejected request", id)
	}
}

func TestWhoamiRejectsPlayerFromOtherClub(t *testing.T) {
	e := newEnv(t)
	a, b := e.club("a"), e.club("b")
	e.player(a, "Alice", false)
	bob := e.player(b, "Bob", false)

	rec := e.do("POST", "/c/a/whoami", nil, a, url.Values{"player_id": {strconv.FormatInt(bob.ID, 10)}})
	wantStatus(t, rec, http.StatusBadRequest)
}

func TestCookieIsNotTrustedBlindly(t *testing.T) {
	e := newEnv(t)
	a, b := e.club("a"), e.club("b")
	admin := e.player(a, "Admin", true)
	e.player(b, "Bob", true)

	get := func(path, value string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.AddCookie(&http.Cookie{Name: cookieName("a"), Value: value})
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec
	}
	valid := e.login(a, &admin).Value
	wantStatus(t, get("/c/a/admin", valid), 200)

	// Unsigned, tampered and garbage values all count as "nobody".
	idStr := strconv.FormatInt(admin.ID, 10)
	for _, bad := range []string{idStr, idStr + ".AAAA", "2." + strings.SplitN(valid, ".", 2)[1], "junk", ""} {
		wantStatus(t, get("/c/a/admin", bad), http.StatusSeeOther)
	}

	// A cookie signed for club a is useless in club b even if the ID exists there.
	req := httptest.NewRequest("GET", "/c/b/admin", nil)
	req.AddCookie(&http.Cookie{Name: cookieName("b"), Value: valid})
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	wantStatus(t, rec, http.StatusSeeOther)
}

func TestAdminAccess(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")
	admin := e.player(c, "Admin", true)
	member := e.player(c, "Member", false)

	rec := e.do("GET", "/c/friends/admin", nil, c, nil)
	wantStatus(t, rec, http.StatusSeeOther)
	require.Equal(t, "/c/friends/whoami", rec.Header().Get("Location"), "anonymous redirect")
	wantStatus(t, e.do("GET", "/c/friends/admin", &member, c, nil), 403)
	wantStatus(t, e.do("POST", "/c/friends/admin/players", &member, c,
		url.Values{"action": {"add"}, "name": {"Sneaky"}}), 403)
	wantStatus(t, e.do("GET", "/c/friends/admin", &admin, c, nil), 200)

	ps, _ := e.players.List(context.Background(), c.ID)
	require.Len(t, ps, 2, "non-admin POST changed players")
}

func TestAdminPlayerManagement(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")
	admin := e.player(c, "Admin", true)
	post := func(f url.Values) *httptest.ResponseRecorder {
		return e.do("POST", "/c/friends/admin/players", &admin, c, f)
	}
	list := func() []domain.Player {
		ps, err := e.players.List(context.Background(), c.ID)
		require.NoError(t, err)
		return ps
	}
	find := func(name string) domain.Player {
		for _, p := range list() {
			if p.Name == name {
				return p
			}
		}
		require.FailNow(t, "no such player", "no player %q", name)
		return domain.Player{}
	}

	wantStatus(t, post(url.Values{"action": {"add"}, "name": {"SAM"}}), http.StatusSeeOther)
	sam := find("sam")
	require.False(t, sam.IsAdmin)
	require.True(t, sam.Active)
	id := strconv.FormatInt(sam.ID, 10)

	wantStatus(t, post(url.Values{"action": {"grant_admin"}, "player_id": {id}}), http.StatusSeeOther)
	require.True(t, find("sam").IsAdmin, "grant_admin did not stick")
	wantStatus(t, post(url.Values{"action": {"revoke_admin"}, "player_id": {id}}), http.StatusSeeOther)
	wantStatus(t, post(url.Values{"action": {"deactivate"}, "player_id": {id}}), http.StatusSeeOther)
	require.False(t, find("sam").Active, "deactivate did not stick")
	wantStatus(t, post(url.Values{"action": {"activate"}, "player_id": {id}}), http.StatusSeeOther)

	// A retired player's cookie stops working.
	samCookie := e.login(c, &sam)
	wantStatus(t, post(url.Values{"action": {"deactivate"}, "player_id": {id}}), http.StatusSeeOther)
	req := httptest.NewRequest("GET", "/c/friends", nil)
	req.AddCookie(samCookie)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	require.NotContains(t, rec.Body.String(), "Hi sam", "retired player still recognised")

	// Errors re-render the page with a message and change nothing.
	rec = post(url.Values{"action": {"add"}, "name": {"Sam"}})
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	require.Contains(t, rec.Body.String(), "already exists")
	wantStatus(t, post(url.Values{"action": {"add"}, "name": {" "}}), http.StatusUnprocessableEntity)
	rec = post(url.Values{"action": {"revoke_admin"}, "player_id": {strconv.FormatInt(admin.ID, 10)}})
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	require.Contains(t, rec.Body.String(), "at least one active admin")
	wantStatus(t, post(url.Values{"action": {"explode"}}), http.StatusBadRequest)
	wantStatus(t, post(url.Values{"action": {"deactivate"}, "player_id": {"nope"}}), http.StatusNotFound)
}

func TestAdminCannotTouchOtherClubsPlayers(t *testing.T) {
	e := newEnv(t)
	a, b := e.club("a"), e.club("b")
	adminA := e.player(a, "Alice", true)
	bob := e.player(b, "Bob", true)
	e.player(b, "Extra", true)

	rec := e.do("POST", "/c/a/admin/players", &adminA, a,
		url.Values{"action": {"deactivate"}, "player_id": {strconv.FormatInt(bob.ID, 10)}})
	wantStatus(t, rec, http.StatusNotFound)
	got, _ := e.players.Get(context.Background(), b.ID, bob.ID)
	require.True(t, got.Active, "club a's admin deactivated club b's player")

	rec = e.do("GET", "/c/a/admin", &adminA, a, nil)
	require.NotContains(t, rec.Body.String(), "bob", "admin page leaks other club's players")
}

func TestCrossOriginPostRejected(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")
	sam := e.player(c, "Sam", false)

	req := httptest.NewRequest("POST", "/c/friends/whoami",
		strings.NewReader(url.Values{"player_id": {strconv.FormatInt(sam.ID, 10)}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	wantStatus(t, rec, 403)
}

func TestStaticCSS(t *testing.T) {
	e := newEnv(t)
	rec := e.do("GET", "/static/style.css", nil, domain.Club{}, nil)
	wantStatus(t, rec, 200)
}

func TestRoundKickoffAndTheme(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")
	admin := e.player(c, "Admin", true)
	sam := e.player(c, "Sam", false)

	start := func(as *domain.Player, month string) *httptest.ResponseRecorder {
		return e.do("POST", "/c/friends/admin/rounds", as, c, url.Values{"month": {month}})
	}

	// Only admins can start rounds, and the month has to be valid.
	wantStatus(t, start(&sam, "2026-03"), 403)
	wantStatus(t, start(nil, "2026-03"), http.StatusSeeOther)
	wantStatus(t, start(&admin, "March"), http.StatusUnprocessableEntity)
	wantStatus(t, e.do("GET", "/c/friends/rounds/2026-03", &sam, c, nil), 404)

	wantStatus(t, start(&admin, "2026-03"), http.StatusSeeOther)
	rec := start(&admin, "2026-04")
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	require.Contains(t, rec.Body.String(), "already has a round in progress")

	// Both players are in the first cycle's pool, so either may be picker; find out who.
	round, err := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NoError(t, err)
	picker, other := admin, sam
	if round.PickerID == sam.ID {
		picker, other = sam, admin
	}

	rec = e.do("GET", "/c/friends/rounds/2026-03", &other, c, nil)
	wantStatus(t, rec, 200)
	body := rec.Body.String()
	require.Contains(t, body, "Waiting on <strong>"+picker.Name+"</strong>")
	require.NotContains(t, body, `name="theme"`, "non-picker should not see the form")
	rec = e.do("GET", "/c/friends/rounds/2026-03", &picker, c, nil)
	require.Contains(t, rec.Body.String(), `name="theme"`, "picker should see the theme form")
	rec = e.do("GET", "/c/friends", &other, c, nil)
	require.Contains(t, rec.Body.String(), "waiting on "+picker.Name, "home should show the pending round")
	rec = e.do("GET", "/c/friends", &picker, c, nil)
	require.Contains(t, rec.Body.String(), "your turn to pick the theme", "home should prompt the picker")

	// Theme rules: logged in, the picker, non-empty.
	theme := func(as *domain.Player, th string) *httptest.ResponseRecorder {
		return e.do("POST", "/c/friends/rounds/2026-03/theme", as, c, url.Values{"theme": {th}, "description": {"wet"}})
	}
	wantStatus(t, theme(nil, "Rain"), http.StatusSeeOther)
	wantStatus(t, theme(&other, "Rain"), 403)
	wantStatus(t, theme(&picker, "  "), http.StatusUnprocessableEntity)
	r, _ := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.Equal(t, domain.PhaseThemePending, r.Phase, "rejected theme changed the round")
	wantStatus(t, theme(&picker, "Rain"), http.StatusSeeOther)
	wantStatus(t, theme(&picker, "Sun"), http.StatusConflict)

	rec = e.do("GET", "/c/friends/rounds/2026-03", &other, c, nil)
	require.Contains(t, rec.Body.String(), "Rain")
	require.Contains(t, rec.Body.String(), "Submissions open until")
	// Re-roll is no longer possible once the theme is set.
	wantStatus(t, e.do("POST", "/c/friends/admin/rounds/2026-03/reroll", &admin, c, url.Values{}), http.StatusConflict)
}

func TestAdminReroll(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")
	admin := e.player(c, "Admin", true)
	sam := e.player(c, "Sam", false)

	wantStatus(t, e.do("POST", "/c/friends/admin/rounds", &admin, c, url.Values{"month": {"2026-03"}}), http.StatusSeeOther)
	before, _ := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")

	wantStatus(t, e.do("POST", "/c/friends/admin/rounds/2026-03/reroll", &sam, c, url.Values{}), 403)
	wantStatus(t, e.do("POST", "/c/friends/admin/rounds/2026-09/reroll", &admin, c, url.Values{}), 404)
	wantStatus(t, e.do("POST", "/c/friends/admin/rounds/2026-03/reroll", &admin, c, url.Values{}), http.StatusSeeOther)
	after, _ := e.rounds.ByMonth(context.Background(), c.ID, "2026-03")
	require.NotEqual(t, before.PickerID, after.PickerID, "picker unchanged after re-roll")
}

func TestRoundsAreClubScopedOnTheWeb(t *testing.T) {
	e := newEnv(t)
	a, b := e.club("a"), e.club("b")
	adminA := e.player(a, "Alice", true)
	e.player(b, "Bob", true)

	wantStatus(t, e.do("POST", "/c/a/admin/rounds", &adminA, a, url.Values{"month": {"2026-03"}}), http.StatusSeeOther)
	wantStatus(t, e.do("GET", "/c/a/rounds/2026-03", &adminA, a, nil), 200)
	wantStatus(t, e.do("GET", "/c/b/rounds/2026-03", nil, b, nil), 404)
	rec := e.do("GET", "/c/b", nil, b, nil)
	require.Contains(t, rec.Body.String(), "No round in progress", "club b home shows club a's round")
}
