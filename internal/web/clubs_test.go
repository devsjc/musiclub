package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"musiclub/internal/db"
	"musiclub/internal/domain"
	"musiclub/internal/store"
)

type env struct {
	t       *testing.T
	h       http.Handler
	clubs   *store.Clubs
	players *store.Players
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &env{t: t, h: NewServer(d, "test-secret").Handler(), clubs: store.NewClubs(d), players: store.NewPlayers(d)}
}

func (e *env) club(slug string) domain.Club {
	e.t.Helper()
	c, err := e.clubs.Create(context.Background(), slug, slug+" Club")
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

func (e *env) player(c domain.Club, name string, admin bool) domain.Player {
	e.t.Helper()
	p, err := e.players.Create(context.Background(), c.ID, name, admin)
	if err != nil {
		e.t.Fatal(err)
	}
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
	e.t.Fatalf("whoami set no cookie: status %d", rec.Code)
	return nil
}

func wantStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d\n%s", rec.Code, want, rec.Body)
	}
}

func TestIndex(t *testing.T) {
	e := newEnv(t)

	rec := e.do("GET", "/", nil, domain.Club{}, nil)
	wantStatus(t, rec, 200)
	if !strings.Contains(rec.Body.String(), "No clubs yet") {
		t.Fatal("empty index should say there are no clubs")
	}

	e.club("one")
	rec = e.do("GET", "/", nil, domain.Club{}, nil)
	wantStatus(t, rec, http.StatusFound)
	if loc := rec.Header().Get("Location"); loc != "/c/one" {
		t.Fatalf("redirect to %q, want /c/one", loc)
	}

	e.club("two")
	rec = e.do("GET", "/", nil, domain.Club{}, nil)
	wantStatus(t, rec, 200)
	if body := rec.Body.String(); !strings.Contains(body, "/c/one") || !strings.Contains(body, "/c/two") {
		t.Fatalf("index should list both clubs:\n%s", body)
	}
}

func TestClubMiddleware(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")

	wantStatus(t, e.do("GET", "/c/nope", nil, c, nil), 404)
	wantStatus(t, e.do("GET", "/c/nope/whoami", nil, c, nil), 404)

	rec := e.do("GET", "/c/friends", nil, c, nil)
	wantStatus(t, rec, 200)
	if !strings.Contains(rec.Body.String(), "friends club") {
		t.Fatal("home should show the club name")
	}
}

func TestWhoami(t *testing.T) {
	e := newEnv(t)
	c := e.club("friends")
	sam := e.player(c, "Sam", false)
	e.player(c, "Kit", true)
	retired := e.player(c, "Retired", false)
	if err := e.players.SetActive(context.Background(), c.ID, retired.ID, false); err != nil {
		t.Fatal(err)
	}

	rec := e.do("GET", "/c/friends/whoami", nil, c, nil)
	wantStatus(t, rec, 200)
	if body := rec.Body.String(); !strings.Contains(body, "sam") || !strings.Contains(body, "kit") || strings.Contains(body, "retired") {
		t.Fatalf("whoami should list active players only:\n%s", body)
	}

	// Picking a player sets a scoped, HttpOnly cookie and the home page greets them.
	rec = e.do("POST", "/c/friends/whoami", nil, c, url.Values{"player_id": {strconv.FormatInt(sam.ID, 10)}})
	wantStatus(t, rec, http.StatusSeeOther)
	ck := rec.Result().Cookies()[0]
	if ck.Name != "mc_friends" || ck.Path != "/c/friends" || !ck.HttpOnly {
		t.Fatalf("unexpected cookie: %+v", ck)
	}
	rec = e.do("GET", "/c/friends", &sam, c, nil)
	if !strings.Contains(rec.Body.String(), "Hi sam.") {
		t.Fatalf("home should greet sam:\n%s", rec.Body)
	}

	// Retired, unknown and non-numeric players are rejected.
	for _, id := range []string{strconv.FormatInt(retired.ID, 10), "9999", "x", ""} {
		rec = e.do("POST", "/c/friends/whoami", nil, c, url.Values{"player_id": {id}})
		wantStatus(t, rec, http.StatusBadRequest)
		if len(rec.Result().Cookies()) != 0 {
			t.Fatalf("player_id %q: cookie set on rejected request", id)
		}
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
	if loc := rec.Header().Get("Location"); loc != "/c/friends/whoami" {
		t.Fatalf("anonymous redirected to %q", loc)
	}
	wantStatus(t, e.do("GET", "/c/friends/admin", &member, c, nil), 403)
	wantStatus(t, e.do("POST", "/c/friends/admin/players", &member, c,
		url.Values{"action": {"add"}, "name": {"Sneaky"}}), 403)
	wantStatus(t, e.do("GET", "/c/friends/admin", &admin, c, nil), 200)

	if ps, _ := e.players.List(context.Background(), c.ID); len(ps) != 2 {
		t.Fatalf("non-admin POST changed players: %+v", ps)
	}
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
		if err != nil {
			t.Fatal(err)
		}
		return ps
	}
	find := func(name string) domain.Player {
		for _, p := range list() {
			if p.Name == name {
				return p
			}
		}
		t.Fatalf("no player %q", name)
		return domain.Player{}
	}

	wantStatus(t, post(url.Values{"action": {"add"}, "name": {"SAM"}}), http.StatusSeeOther)
	sam := find("sam")
	if sam.IsAdmin || !sam.Active {
		t.Fatalf("new player = %+v", sam)
	}
	id := strconv.FormatInt(sam.ID, 10)

	wantStatus(t, post(url.Values{"action": {"grant_admin"}, "player_id": {id}}), http.StatusSeeOther)
	if !find("sam").IsAdmin {
		t.Fatal("grant_admin did not stick")
	}
	wantStatus(t, post(url.Values{"action": {"revoke_admin"}, "player_id": {id}}), http.StatusSeeOther)
	wantStatus(t, post(url.Values{"action": {"deactivate"}, "player_id": {id}}), http.StatusSeeOther)
	if find("sam").Active {
		t.Fatal("deactivate did not stick")
	}
	wantStatus(t, post(url.Values{"action": {"activate"}, "player_id": {id}}), http.StatusSeeOther)

	// A retired player's cookie stops working.
	samCookie := e.login(c, &sam)
	wantStatus(t, post(url.Values{"action": {"deactivate"}, "player_id": {id}}), http.StatusSeeOther)
	req := httptest.NewRequest("GET", "/c/friends", nil)
	req.AddCookie(samCookie)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), "Hi sam") {
		t.Fatal("retired player still recognised")
	}

	// Errors re-render the page with a message and change nothing.
	rec = post(url.Values{"action": {"add"}, "name": {"Sam"}})
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if !strings.Contains(rec.Body.String(), "already exists") {
		t.Fatalf("duplicate name message missing:\n%s", rec.Body)
	}
	wantStatus(t, post(url.Values{"action": {"add"}, "name": {" "}}), http.StatusUnprocessableEntity)
	rec = post(url.Values{"action": {"revoke_admin"}, "player_id": {strconv.FormatInt(admin.ID, 10)}})
	wantStatus(t, rec, http.StatusUnprocessableEntity)
	if !strings.Contains(rec.Body.String(), "at least one active admin") {
		t.Fatalf("last-admin message missing:\n%s", rec.Body)
	}
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
	if got, _ := e.players.Get(context.Background(), b.ID, bob.ID); !got.Active {
		t.Fatal("club a's admin deactivated club b's player")
	}

	rec = e.do("GET", "/c/a/admin", &adminA, a, nil)
	if strings.Contains(rec.Body.String(), "bob") {
		t.Fatal("admin page leaks other club's players")
	}
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
