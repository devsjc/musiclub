# Music Club: Design Document

A small web app for groups of friends running a monthly music club. Each month one member picks a theme, everyone submits a song, everyone listens, votes and guesses who chose what, and points are tallied. The app supports multiple independent groups ("clubs") from day one, though it is expected to start with one.

## 1. Goals and Non-Goals

**Goals**
- Single Docker container, single binary, single SQLite file.
- Server-rendered HTML with HTMX for interactivity. No JS build step, no SPA.
- Small codebase a single person can hold in their head.
- Scoring rules isolated in one pure, well-tested package so they can change without touching anything else.
- Every piece of data and every URL is scoped to a club, so adding a second group later needs no migration or refactor.
- Song link matching (YouTube Music) is a convenience, never a dependency: if every external service is down or unconfigured, players can still submit by pasting a URL.

**Non-goals (for now)**
- Authentication and accounts (see §8.4 for a stopgap).
- People belonging to several clubs under one identity (a person in two clubs is simply two `players` rows).
- Embedded players, streaming or playlist export (the app matches and stores links, but players listen on the service itself).
- Horizontal scaling. One process, one writer.

## 2. Tech Stack

| Concern | Choice | Why |
|---|---|---|
| Language | Go (1.22+) | Single static binary, great stdlib |
| Router | `net/http` stdlib `ServeMux` (method + path patterns) | No dependency needed since Go 1.22 |
| DB | SQLite via `modernc.org/sqlite` (pure Go) | No CGO, so a trivial distroless image |
| DB access | `database/sql` + hand-written queries (optionally `sqlc` later) | Explicit, easy to read |
| Migrations | Embedded `.sql` files + tiny runner (or `pressly/goose`) | Runs at startup |
| Templates | `templ` (recommended) or `html/template` | Type-safe components; `html/template` is fine if you want zero codegen |
| Interactivity | HTMX (vendored, served from `/static`) | Partial swaps, no client state |
| Styling | One small hand-written CSS file (or Pico.css / Simple.css) | Keep it light |
| Music link lookup | Unofficial YouTube Music client (`ytmusicapi`) behind a small `Provider` interface (§5.5) | Optional; the app works without it |

## 3. The Rules (as implemented)

**Submitting**
1. Each player submits exactly one song per round. Once submitted, it cannot be changed, though admins can delete a submission if needed.
2. Duplicate songs are allowed. Duplicates are detected by a normalised `song_key` (§5.3).

Every submission must carry a listening link. The server suggests matches for the typed artist and title (YouTube Music first, see §5.5); the player confirms one, or if none is right, pastes their own URL.

**Voting**
3. Each player casts exactly one **Favourite Song** vote and exactly one **Best Fit for Theme** vote.
4. The two votes must be for different songs.
5. Each player also guesses who submitted each song, as a **full assignment**: every song other than their own is mapped to a distinct player (no player is used twice).
6. Reveals (who submitted what, tallies, guesses, comments) only appear after the round is closed.

**Points** (all awarded to the song's submitter unless stated)
| Event | Points |
|---|---|
| Each vote cast for your song, in either category | +1 per vote |
| Having the most votes in a category (ties share it) | +1 per category won |
| **Trendsetter:** you were the first to submit a song that others also submitted | +1 per other player who submitted the same song |

Guesses do **not** earn points; they feed statistics only.

**Tracked each month and cumulatively**
- Winner(s) of **Best Song** (most favourite votes), **Best Theme Fit** (most best-fit votes) and **Overall** (most votes combined across both categories).
- Correct-guess percentage, plus who is easiest and hardest to guess.
- Points leaderboard.

## 4. Lifecycle

A **Round** belongs to one club and moves through a linear state machine:

```
 theme_pending ──► submitting ──► voting ──► review ──► closed
      │                │             │          │          │
  admin kicks off  picker sets    all songs   admin picks  results public,
  round; picker    theme; 2-week  are in;     favourite    scores written,
  chosen randomly  window opens   listen+vote  quotes       stats updated
```

| Phase | Who acts | What happens |
|---|---|---|
| `theme_pending` | Admin, then picker | Admin starts the round; a picker is chosen at random (§5.4). The picker sets the theme. |
| `submitting` | Everyone | ~2 weeks. One song per player. Submitting is two steps: enter artist + title, then confirm a suggested link match or paste a URL (§5.5). Players can see who has submitted, not what. |
| `voting` | Everyone | Remaining ~2 weeks. Anonymous, shuffled list. Each player fills in one ballot. |
| `review` | Admin only | Voting is over. Admin sees tallies, fixes any wrongly detected duplicates, and picks favourite quotes for the winning songs. Players see "results coming soon". |
| `closed` | Nobody | Admin presses **Publish**. Scores are written once; results and stats are visible to everyone. |

A club has at most one round that isn't `closed` at a time; starting another is refused until it is published.

Transitions:
- Admin starts round → `theme_pending` (round row created, picker chosen).
- Picker submits theme → `submitting` (deadline = now + `submission_days`).
- Submission deadline passes **or** all active players have submitted → `voting` (deadline = now + `voting_days`).
- Voting deadline passes **or** all active players have voted → `review`.
- Admin publishes → `closed`.

The scheduler **never creates rounds and never publishes**; it only advances `submitting → voting → review`. `Tick` is idempotent and also runs lazily on page loads, so correctness never depends on the ticker alone.

## 5. Architecture

```
┌────────────────────────── Docker container ──────────────────────────┐
│                                                                      │
│  Browser ──HTTP──► net/http server  (/c/{club}/...)                  │
│   (HTMX)             │                                               │
│                      ▼                                               │
│               ┌─────────────┐   renders    ┌──────────────┐          │
│               │  handlers   │─────────────►│  templ views │          │
│               └──────┬──────┘              └──────────────┘          │
│                      │ calls                                         │
│        ┌─────────────┼──────────────┐                                │
│        ▼             ▼              ▼                                │
│   ┌─────────┐  ┌───────────┐  ┌───────────┐                         │
│   │  store  │  │  rounds   │  │  scoring  │  (pure functions)       │
│   │ (SQL)   │  │ (lifecycle│  │  + stats  │                         │
│   └────┬────┘  │  service) │  └───────────┘                         │
│        │       └─────┬─────┘                                         │
│        ▼             ▲                                               │
│   SQLite file    scheduler goroutine (ticker)                        │
│   /data/club.db                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

Layering rule: dependencies point downward only. `scoring` depends on nothing but plain structs; `store` knows SQL; `handlers` know HTTP; nothing imports `handlers`.

**Club scoping rule:** a middleware resolves `{club}` (slug) to a `Club` once per request and puts it on the context. Every `store` function that touches club data takes a `clubID` (or a `roundID` already verified to belong to that club). There is no query that reads across clubs except the club list.

### 5.1 Project Layout

```
musiclub/
├── cmd/
│   └── musiclub/
│       └── main.go            # wiring; also tiny subcommands: `club add`, `healthcheck`
├── internal/
│   ├── config/                # env var parsing
│   ├── db/
│   │   ├── db.go              # open SQLite, pragmas, run migrations
│   │   └── migrations/
│   │       ├── 0001_init.sql
│   │       └── ...
│   ├── domain/                # plain structs + enums (Club, Round, Submission, Ballot...)
│   ├── store/                 # all SQL lives here; one file per aggregate
│   │   ├── clubs.go
│   │   ├── players.go
│   │   ├── rounds.go
│   │   ├── submissions.go
│   │   ├── ballots.go
│   │   ├── link_cache.go
│   │   ├── quotes.go
│   │   └── scores.go
│   ├── songkey/               # PURE: normalise artist/title/URL into a duplicate-detection key
│   ├── linkresolver/          # Provider interface, YouTube Music client, cache, URL validation
│   ├── rounds/                # lifecycle service: kickoff, picker, advance phases, publish
│   ├── scoring/               # PURE: Results in -> Events out. Heavily unit tested.
│   ├── stats/                 # read-side queries for monthly + cumulative views
│   ├── web/
│   │   ├── server.go          # mux, middleware (logging, recover, club, current-player, admin)
│   │   ├── handlers_*.go      # one file per area
│   │   └── views/             # *.templ (or *.html) components
│   └── scheduler/             # ticker that calls rounds.Tick(now) for every club
├── static/
│   ├── htmx.min.js
│   └── style.css
├── Dockerfile
├── Makefile
└── go.mod
```

Static files and migrations are embedded with `//go:embed`, so the binary is fully self-contained.

### 5.2 SQLite Configuration

Set on open: `journal_mode=WAL`, `foreign_keys=ON`, `busy_timeout=5000`, `synchronous=NORMAL`. Use a single `*sql.DB` with `SetMaxOpenConns(1)` for writes (or a separate read pool). At this scale a single connection is plenty and avoids `SQLITE_BUSY` entirely.

### 5.3 Duplicate Detection (`internal/songkey`)

Trendsetter points depend on knowing two submissions are "the same song", so this is worth making deliberate.

- `song_key` is computed on every submit from **artist + title**, normalised: lowercase, Unicode-folded, punctuation stripped, and noise removed (`feat. ...`, `(Remastered 2011)`, `- Single Version`, etc.). Artist+title is used rather than the URL because the same song on Spotify and YouTube has different URLs.
- The key is stored on the submission and is **admin-editable during `review`**, so a missed duplicate (typo, different spelling) can be merged, or a false positive split, before points are written.
- **Canonical metadata beats typing.** When the player confirms a matched candidate, the stored `artist`/`title` (and therefore the `song_key`) come from the service's metadata rather than the player's typing, so two players who pick the same track end up with the same key even if they typed it differently. The matched `external_id` (§5.5) is kept too, and the review page flags submissions that share an `external_id` but have different keys.
- **Order:** the "first" submitter is determined by `submissions.submitted_at`. Ties on timestamp fall back to lowest `id`.
- **No duplicate warning at submit time.** Telling a player "someone has already picked this" would leak information about the other submissions before voting. Duplicates are only surfaced to the admin during review.

### 5.4 Theme-Picker Selection

"Randomly selected" should still be fair, so each club rotates through a **cycle**: pick uniformly at random among active players who haven't picked in the current cycle. When everyone has picked, a new cycle starts.

- `rounds.picker_id` and `rounds.cycle` record who picked and in which cycle.
- Selection query (per club): active players whose id is not in `SELECT picker_id FROM rounds WHERE club_id = :club AND cycle = :current`; choose with `math/rand` in Go.
- Runs when the admin kicks off a round. The admin can **re-roll** the picker while the round is still `theme_pending` (e.g. someone is on holiday).

### 5.5 Link Matching (`internal/linkresolver`)

During the submission stage the server looks up the song the player typed and offers matching listening links, so voters get a one-click "Listen" button instead of a hand-pasted URL of uneven quality.

**The provider is the unofficial YouTube Music client** [`github.com/prettyirrelevant/ytmusicapi`](https://pkg.go.dev/github.com/prettyirrelevant/ytmusicapi) (a Go take on Python's `ytmusicapi`). There is no official YouTube Music API; the client emulates the web app using headers copied from a logged-in browser, so it can break without notice and sits in a grey area of YouTube's terms. That is acceptable here because matching is a convenience (§1), and everything sits behind a one-method `Provider` interface so it can be swapped.

- **Setup:** follow the library's README to turn a copied request into `ytmusic_headers.json`, and pass its contents in `YTMUSIC_HEADERS_JSON` (§12). The value carries a logged-in Google session cookie: treat it as a secret, and prefer a throwaway account.
- **What it returns:** only the video ID, title and artists for each result, with no album, thumbnail or duration. Searches use the "songs" filter and every result links to `https://music.youtube.com/watch?v={id}`.
- **Library quirks the code works around:** `Setup()` panics on missing or bad headers, so `SetupYTMusic` validates them first and the app starts without matching if they are unset; it cannot find `__Secure-3PAPISID` unless that cookie is first, so the cookie header is normalised; it takes no `context` and parses with unchecked indexing, so each search runs in a goroutine that the 3s deadline can abandon and that turns a panic into an error.

```go
package linkresolver

type Match struct {
    ExternalID string // video ID
    URL        string // https://music.youtube.com/watch?v={id}
    Title      string
    Artist     string
}

type Provider interface {
    Search(ctx context.Context, artist, title string) ([]Match, error)
}

type Resolver struct { /* provider, cache, limiter */ }

// Candidates: <= 2 results, 3s timeout, cache first. No provider: no candidates.
func (r *Resolver) Candidates(ctx context.Context, artist, title string) ([]Match, error)
```

With no headers configured, the flow below collapses to the manual URL box.

**Flow**

```
 Player            Browser (HTMX)             Server                       YouTube Music
   │  artist+title      │                        │                               │
   ├───────────────────►│ POST …/submission/search                               │
   │                    ├───────────────────────►│ cache? ─ miss ─► Search ──────►│ YT Music
   │                    │◄── candidate cards ────┤◄──────────────────────────────┤
   │  pick one          │                        │
   │  (or "none of these → paste my own URL")    │
   ├───────────────────►│ POST …/submission      │
   │                    ├───────────────────────►│ validate, save submission
   │                    │◄── "saved ✓" ──────────┤
```

1. The player fills in artist and title and presses **Find song**. (A button, not search-as-you-type, to save API quota.)
2. `POST …/submission/search` returns an HTMX fragment of up to two candidate cards: artist, title and a "preview" link that opens in a new tab. Each card is a radio option carrying its data in hidden fields.
3. The last option is always **"None of these, I'll paste my own link"**, which reveals a URL input (a small `hx-get` fragment).
4. `POST …/submission` saves the song. Choosing a candidate stores `link_source = 'matched'` plus the candidate's canonical artist/title (§5.3). Pasting stores `link_source = 'manual'` with the player's own artist/title.
5. During voting each song shows a **Listen** button (`submissions.url`).

**Graceful degradation.** If the provider is unconfigured, errors, times out or returns nothing, the fragment says "Couldn't find matches, paste a link instead" and shows the URL box directly. Submission is never blocked by an external service.

**Trust and validation (server-side, because candidates round-trip through hidden fields)**
- URLs must parse and use `http` or `https`; `javascript:`, `data:` and the like are rejected; length is capped.
- A `matched` link must start with `https://music.youtube.com/watch?v=`, the only form the provider produces; anything else is refused.
- For `manual` links any http(s) host is accepted (Bandcamp, SoundCloud, ...). The server never fetches a pasted URL, which avoids SSRF. Known hosts (YouTube Music, Spotify, Qobuz) are classified into `url_service` and tracking parameters such as `si=` are stripped; anything else is `other`.
- All stored links are rendered with `target="_blank" rel="noopener noreferrer nofollow"`.

**Anonymity.** Search results go only to the player searching. The cache is internal and never displayed, so nothing about other players' songs leaks during the submission stage.

**Caching and rate limiting.** Provider responses are cached in `link_cache` keyed by `("ytmusic", normalised "artist|title")` with a TTL (default 7 days). A process-wide semaphore caps concurrent outbound calls. At ~10 players a month this is more than enough.

## 6. Database Schema

```sql
-- 0001_init.sql

CREATE TABLE clubs (
    id               INTEGER PRIMARY KEY,
    slug             TEXT    NOT NULL UNIQUE,         -- used in URLs: /c/{slug}/...
    name             TEXT    NOT NULL,
    submission_days  INTEGER NOT NULL DEFAULT 14,     -- per-club window lengths
    voting_days      INTEGER NOT NULL DEFAULT 14,
    created_at       TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE players (
    id          INTEGER PRIMARY KEY,
    club_id     INTEGER NOT NULL REFERENCES clubs(id) ON DELETE CASCADE,
    name        TEXT    NOT NULL,
    active      INTEGER NOT NULL DEFAULT 1,           -- 0 = retired from future rounds
    is_admin    INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE (club_id, name)
);

CREATE TABLE rounds (
    id                    INTEGER PRIMARY KEY,
    club_id               INTEGER NOT NULL REFERENCES clubs(id) ON DELETE CASCADE,
    month                 TEXT    NOT NULL,           -- 'YYYY-MM'
    cycle                 INTEGER NOT NULL,           -- picker rotation cycle (per club)
    picker_id             INTEGER NOT NULL REFERENCES players(id),
    theme                 TEXT,                       -- NULL until picker sets it
    phase                 TEXT    NOT NULL DEFAULT 'theme_pending'
                          CHECK (phase IN ('theme_pending','submitting','voting','review','closed')),
    submissions_due_at    TEXT,                       -- set when theme is chosen
    voting_due_at         TEXT,                       -- set when voting opens
    created_at            TEXT    NOT NULL DEFAULT (datetime('now')),
    closed_at             TEXT,
    UNIQUE (club_id, month)
);

CREATE TABLE submissions (
    id            INTEGER PRIMARY KEY,
    round_id      INTEGER NOT NULL REFERENCES rounds(id)  ON DELETE CASCADE,
    player_id     INTEGER NOT NULL REFERENCES players(id),
    title         TEXT    NOT NULL,
    artist        TEXT    NOT NULL,
    url           TEXT    NOT NULL,                   -- primary listening link; always present (§5.5)
    url_service   TEXT    NOT NULL DEFAULT 'other'
                  CHECK (url_service IN ('youtube_music','spotify','qobuz','other')),
    link_source   TEXT    NOT NULL DEFAULT 'manual'
                  CHECK (link_source IN ('matched','manual')),   -- confirmed suggestion vs pasted URL
    external_id   TEXT,                               -- the service's track/video ID when matched
    note          TEXT,                               -- optional blurb by submitter
    song_key      TEXT    NOT NULL,                   -- normalised duplicate key (§5.3), admin-editable in review
    submitted_at  TEXT    NOT NULL DEFAULT (datetime('now')),
    position      INTEGER,                            -- shuffled display order, set when voting opens
    UNIQUE (round_id, player_id)                      -- rule 1: one song per player per round
);

-- One ballot per voter per round.
CREATE TABLE ballots (
    id                        INTEGER PRIMARY KEY,
    round_id                  INTEGER NOT NULL REFERENCES rounds(id)  ON DELETE CASCADE,
    voter_id                  INTEGER NOT NULL REFERENCES players(id),
    favorite_submission_id    INTEGER NOT NULL REFERENCES submissions(id),
    favorite_comment          TEXT,
    best_fit_submission_id    INTEGER NOT NULL REFERENCES submissions(id),
    best_fit_comment          TEXT,
    submitted_at              TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE (round_id, voter_id),
    CHECK (favorite_submission_id <> best_fit_submission_id)   -- rule 4
);

-- Who the voter thinks submitted each song. Full assignment = one row per song
-- (excluding the voter's own), each guessed player used at most once.
CREATE TABLE guesses (
    ballot_id           INTEGER NOT NULL REFERENCES ballots(id)     ON DELETE CASCADE,
    submission_id       INTEGER NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    guessed_player_id   INTEGER NOT NULL REFERENCES players(id),
    PRIMARY KEY (ballot_id, submission_id),
    UNIQUE (ballot_id, guessed_player_id)                       -- rule 5: no player guessed twice
);

-- Cache of provider search responses (saves quota and latency). Safe to delete at any time.
CREATE TABLE link_cache (
    provider    TEXT NOT NULL,
    query_key   TEXT NOT NULL,                  -- normalised "artist|title"
    response    TEXT NOT NULL,                  -- JSON array of matches
    fetched_at  TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (provider, query_key)
);

-- Materialised scoring output, written when a round is published.
-- Recomputable from raw data (admin "recompute" action).
CREATE TABLE score_events (
    id              INTEGER PRIMARY KEY,
    round_id        INTEGER NOT NULL REFERENCES rounds(id)  ON DELETE CASCADE,
    player_id       INTEGER NOT NULL REFERENCES players(id),   -- who receives the points
    kind            TEXT    NOT NULL
                    CHECK (kind IN ('favorite_vote','best_fit_vote',
                                    'favorite_winner','best_fit_winner','trendsetter')),
    points          INTEGER NOT NULL,
    submission_id   INTEGER REFERENCES submissions(id),        -- the song involved
    ballot_id       INTEGER REFERENCES ballots(id)             -- the vote involved (vote events)
);

-- Admin-curated quotes shown on the results screen for the winning songs.
CREATE TABLE featured_quotes (
    id             INTEGER PRIMARY KEY,
    round_id       INTEGER NOT NULL REFERENCES rounds(id)      ON DELETE CASCADE,
    category       TEXT    NOT NULL CHECK (category IN ('favorite','best_fit')),
    submission_id  INTEGER NOT NULL REFERENCES submissions(id) ON DELETE CASCADE,
    ballot_id      INTEGER REFERENCES ballots(id) ON DELETE SET NULL,  -- where it came from
    quote          TEXT    NOT NULL,                           -- snapshot; admin may trim it
    position       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_players_club      ON players(club_id);
CREATE INDEX idx_rounds_club       ON rounds(club_id);
CREATE INDEX idx_submissions_round ON submissions(round_id);
CREATE INDEX idx_submissions_key   ON submissions(round_id, song_key);
CREATE INDEX idx_submissions_extid ON submissions(round_id, external_id);
CREATE INDEX idx_ballots_round     ON ballots(round_id);
CREATE INDEX idx_score_round       ON score_events(round_id);
CREATE INDEX idx_score_player      ON score_events(player_id);
CREATE INDEX idx_quotes_round      ON featured_quotes(round_id);
```

**Why `club_id` only on `players` and `rounds`:** everything else hangs off a round, so it is club-scoped transitively. This avoids redundant columns that could disagree. The service layer verifies that every player referenced in a round belongs to that round's club.

### 6.1 Integrity Rules Enforced in the Service Layer

SQL constraints cover rules 1, 4 and the "no player guessed twice" half of rule 5. The rest is enforced in `rounds` before any write:

- Writes to submissions only in `submitting`; ballots only in `voting`; quotes and song-key edits only in `review`.
- A voter cannot vote for their own song in either category *(assumption, see §13)*.
- **Full-assignment check:** let `S` = the set of submitters in the round. The ballot must contain exactly one guess for every submission except the voter's own, and the guessed players must be exactly `S` minus the voter (a bijection). A voter who did not submit a song guesses all `|S|` songs against all `|S|` submitters.
- Ballots are final once cast.
- Quotes can only be attached to a winning submission, and only in the category it won.
- **Link validation** (scheme, host allowlist for matched links, length cap, tracking-param stripping) runs before any submission is saved; a submission cannot exist without a valid primary `url` (§5.5).

## 7. Scoring Module (`internal/scoring`)

Pure, so rules can be changed and tested without a database.

```go
package scoring

type Results struct {
    Submissions []Submission // ID, SubmitterID, SongKey, SubmittedAt
    Ballots     []Ballot     // ID, VoterID, FavoriteID, BestFitID
}

type Event struct {
    PlayerID     PlayerID
    Kind         Kind  // favorite_vote, best_fit_vote, favorite_winner, best_fit_winner, trendsetter
    Points       int
    SubmissionID *SubmissionID
    BallotID     *BallotID
}

type Rules struct {
    VotePoints         int // 1
    CategoryWinBonus   int // 1 (per category won)
    TrendsetterPerDup  int // 1
}

// Score is the single entry point.
func Score(r Results, rules Rules) []Event
```

Each rule is a small function `func(Results, Rules) []Event`; `Score` concatenates them.

| Rule func | Logic |
|---|---|
| `votePoints` | For each ballot, one `favorite_vote` event and one `best_fit_vote` event to the voted songs' submitters, each linked to the `BallotID`. |
| `categoryWinners` | Per category, find the max vote count; every submission at the max gets a `*_winner` event. Nothing is awarded if the max is 0. Ties share the bonus. |
| `trendsetter` | Group submissions by `SongKey`. In each group with more than one entry, the earliest (`SubmittedAt`, then ID) receives one `trendsetter` event per *other* entry, linked to that other submission. |

The same winner-finding helper is reused by `stats` for the Best Song / Best Theme / Overall winners, so the scoring and the displayed winners can never disagree.

## 8. Routes and HTMX Interaction

Everything club-specific lives under `/c/{club}`. Full-page routes render complete HTML. HTMX endpoints return **fragments**; handlers check `HX-Request` to return either the fragment or the full page, so every URL still works without JS.

### 8.1 Routes

| Method | Path | Purpose |
|---|---|---|
| GET | `/` | List of clubs (redirects straight to the club if there is only one) |
| GET | `/healthz` | Liveness for Docker |
| GET | `/c/{club}` | Club home: current round, phase, countdown |
| GET | `/c/{club}/whoami` | "Who are you?" player picker (§8.4) |
| POST | `/c/{club}/whoami` | Set the player cookie |
| GET | `/c/{club}/rounds` | All rounds with themes and winners |
| GET | `/c/{club}/rounds/{month}` | Round page; content depends on phase (§8.2) |
| POST | `/c/{club}/rounds/{month}/theme` | Picker sets theme (`theme_pending`) |
| POST | `/c/{club}/rounds/{month}/submission/search` | Fragment: candidate link matches for the typed artist + title (§5.5) |
| GET | `/c/{club}/rounds/{month}/submission/manual-url` | Fragment: the "paste my own URL" input |
| POST | `/c/{club}/rounds/{month}/submission` | Create/update the current player's song (matched candidate or manual URL) |
| GET | `/c/{club}/rounds/{month}/vote` | Ballot form (`voting`) |
| POST | `/c/{club}/rounds/{month}/ballot` | Save ballot + guesses |
| GET | `/c/{club}/rounds/{month}/results` | Results (`closed` only; 404/redirect otherwise) |
| GET | `/c/{club}/stats` | Cumulative leaderboard and stats |
| GET | `/c/{club}/stats/players/{id}` | Per-player drill-down |
| GET | `/c/{club}/admin` | Admin dashboard: players, current round |
| POST | `/c/{club}/admin/players` | Add / deactivate players, toggle admin |
| POST | `/c/{club}/admin/rounds` | **Kick off a round** (month; picker chosen randomly) |
| POST | `/c/{club}/admin/rounds/{month}/reroll` | Re-roll picker (`theme_pending` only) |
| GET | `/c/{club}/admin/rounds/{month}/review` | Review page: tallies, duplicates, candidate quotes |
| POST | `/c/{club}/admin/rounds/{month}/submissions/{id}/delete` | Delete a submission (e.g. if a player chose the wrong song) |
| POST | `/c/{club}/admin/rounds/{month}/song-keys` | Edit a submission's `song_key` (merge/split duplicates) |
| POST | `/c/{club}/admin/rounds/{month}/quotes` | Add / remove / reorder featured quotes |
| POST | `/c/{club}/admin/rounds/{month}/publish` | Publish: score, write events, close |
| POST | `/c/{club}/admin/rounds/{month}/recompute` | Re-run scoring for a closed round |

Club creation is deliberately **not** a web route for now: use the CLI (`musiclub club add --slug friends --name "Friday Friends"`), which keeps the exposed surface small. Easy to add a site-admin page later.

### 8.2 Round Page by Phase

| Phase | Shows |
|---|---|
| `theme_pending` | "Waiting on **Sam** to pick a theme." The picker sees the theme form. |
| `submitting` | Theme, deadline countdown, two-step submission form (artist + title, then pick a suggested match or paste a URL; §5.5), "who has submitted" checklist (names only, never songs). |
| `voting` | Anonymised, shuffled song list, each with a Listen button (primary link) ; the ballot form; "who has voted" checklist. |
| `review` | Players: "Voting is closed. Results are coming soon." Admin: link to the review page. |
| `closed` | Full reveal: who chose what, tallies, **featured quotes** for the winning songs, points breakdown, monthly stats. |

### 8.3 HTMX Usage (keep it modest)

- **Submission form:** step 1 (artist + title) `hx-post`s to `…/submission/search` and swaps in the candidate cards (with a "none of these, paste my own link" option that swaps in the URL input); step 2 `hx-post`s the choice and swaps the form for a "saved ✓" version. A loading indicator (`hx-indicator`) covers the up-to-3s lookup.
- **Ballot form:** a single page with three sections: favourite, best fit, guesses. `hx-post` on submit; validation errors come back as a fragment ("favourite and best fit must be different songs").
- **Guess assignment:** each song has a `<select>` of players. On change, `hx-post` to a small endpoint that re-renders the set of selects with already-chosen players disabled in the others, so the full-assignment rule is enforced as you fill it in. The server re-validates on submit regardless.
- **Duplicate-song handling in the ballot:** duplicates appear as separate entries (§13), shuffled like any other.
- **Status checklists:** `hx-get` with `hx-trigger="every 30s"` on the "who has submitted/voted" fragment. This is the only polling.
- **Admin quote picker:** on the review page, each winning song shows all comments for that category as a list; clicking "feature" does an `hx-post` that moves the comment (editable text) into the featured list.
- **Stats:** tabs via `hx-get` + `hx-target="#stats-body"`.

### 8.4 Current Player and Admin Without Auth

For now: `/c/{club}/whoami` shows a list of the club's players; choosing one stores their `player_id` in a signed cookie (one cookie per club, e.g. `mc_{slug}`). A middleware reads it and puts the `Player` on the context. Admin routes additionally require `player.is_admin`. Because the first admin can't be created through the web UI, `musiclub player add --club SLUG --name NAME [--admin]` bootstraps players from the CLI; the admin page refuses to retire or demote a club's last active admin. This is a convenience, not security. Anyone can pick anyone. It does give a clean seam: when real auth is added later, only this middleware changes.

## 9. Statistics

All computed with SQL over the raw tables plus `score_events`, filtered by `club_id` (and `round_id` for monthly views). Nothing is cached; at this size every query is instant. Stats pages only show data from `closed` rounds.

### 9.1 Monthly

- **Winners:** Best Song (most favourite votes), Best Theme Fit (most best-fit votes), and Overall (favourite + best-fit votes combined). Ties list all winners. Shown with submitter, vote counts and featured quotes.
- **Points** per player, broken down by event kind (votes, category bonuses, trendsetter).
- **Guessing:**
  - Overall correct-guess percentage for the round.
  - Per guesser: percentage of their guesses that were correct.
  - **Per submitter ("guessability"):** of all guesses made about this player's song, what percentage named them. Highest = *easiest to guess*, lowest = *hardest to guess*.

A guess is correct when `guesses.guessed_player_id = submissions.player_id` for that submission.

```sql
-- guessability per submitter for one round
SELECT s.player_id,
       COUNT(*)                                                     AS guesses_about_me,
       SUM(g.guessed_player_id = s.player_id)                       AS correct,
       100.0 * SUM(g.guessed_player_id = s.player_id) / COUNT(*)    AS pct
FROM guesses g
JOIN ballots b     ON b.id = g.ballot_id
JOIN submissions s ON s.id = g.submission_id
WHERE b.round_id = :round
GROUP BY s.player_id
ORDER BY pct DESC;
```

### 9.2 Cumulative (per club)

- **Leaderboard:** total points, rounds played, average per round (`score_events` grouped by player).
- **Honours:** number of times each player has won Best Song, Best Theme Fit and Overall; trendsetter total.
- **Guessing:** same three guess stats as monthly, aggregated over all closed rounds. Because the number of players varies per round, also show the chance baseline for context (a random full assignment is correct about 1 in *n−1* of the time).
- **Guess matrix:** "how often does *A* correctly guess *B*'s songs" as a plain HTML table (heatmap by CSS).
- **Points over time:** inline server-rendered SVG, no JS chart library.

`internal/stats` exposes functions like `MonthSummary(roundID)` and `Leaderboard(clubID)` that return view-model structs; templates only render.

## 10. Lifecycle Service (`internal/rounds`)

```go
type Service interface {
    // Admin
    StartRound(ctx, clubID, month string) (Round, error)   // creates row, picks picker
    RerollPicker(ctx, roundID) error                       // theme_pending only
    SetSongKey(ctx, roundID, submissionID, key string) error // review only
    SetQuotes(ctx, roundID, []QuoteInput) error            // review only; validates winners
    Publish(ctx, roundID) error                            // review -> closed

    // Players
    SetTheme(ctx, roundID, pickerID, theme string) error // -> submitting
    SuggestLinks(ctx, artist, title string) ([]Match, error) // read-only, no DB writes
    SubmitSong(ctx, roundID, playerID, Song) error  // Song carries a LinkChoice (matched or manual); validates the URL
    SaveBallot(ctx, roundID, voterID, BallotInput) error

    // Scheduler
    Tick(ctx, now time.Time) error // advances submitting->voting->review; idempotent
}
```

- Entering `voting` assigns a random `position` to each submission, stable for the round but not guessable.
- `Publish` runs in **one transaction**: load results → `scoring.Score` → delete+insert `score_events` → set `phase='closed'`, `closed_at`. It refuses to run if there are no featured quotes for a winning category *unless* the admin explicitly confirms (so results aren't published half-curated by accident).
- `recompute` re-runs the scoring step for a closed round (use after rule changes or a late `song_key` correction).
- `Tick` iterates over all clubs; it takes an injected clock (`now`) for testing.

## 11. Edge Cases

- **Player misses the submission deadline:** has no song, still votes. Their guess set covers all submitters (§6.1).
- **Player misses voting:** earns no voting-derived points from *their* ballot, but their song still receives votes and points.
- **Tied winners:** every tied song gets the category bonus and appears as a joint winner; quotes can be picked for each.
- **Zero votes in a category:** no winner, no bonus.
- **Trendsetter ties on timestamp:** earliest `submitted_at`, then lowest `id`.
- **Three+ identical songs:** the first submitter gets one point per *other* entry (so 2 points for 3 total).
- **Players joining/leaving:** `active` flag plus deriving a round's participants from its submissions and ballots rather than the live player list, so history is stable.
- **Picker unavailable:** admin re-rolls. If they never set a theme, admin can re-roll or delete the empty round.
- **Admin edits `song_key` after publish:** run `recompute`.
- **No provider configured, API down, or quota exhausted:** the form shows only the manual URL input; submitting always works.
- **No good match:** the player chooses "none of these" and pastes a URL.
- **Wrong match confirmed by mistake:** the player must ask the admin to delete their submission so they can submit again.
- **Pasted URL doesn't match the typed artist/title:** accepted as-is. It is not fetched or verified.
- **Same song matched under different IDs** (e.g. an album track and a music-video upload): artist+title normalisation usually still groups them; otherwise the admin merges the `song_key` during review.

## 12. Deployment

### Dockerfile

```dockerfile
FROM golang:1.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# If using templ: RUN go install github.com/a-h/templ/cmd/templ@latest && templ generate
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/musiclub ./cmd/musiclub

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/musiclub /musiclub
VOLUME /data
ENV DB_PATH=/data/club.db PORT=8080 TZ=Europe/London
EXPOSE 8080
ENTRYPOINT ["/musiclub"]
```

Distroless has no shell or `curl`; use `musiclub healthcheck` (a tiny subcommand that GETs `/healthz`) for Docker's `HEALTHCHECK`. Run one-off commands with `docker exec <ctr> /musiclub club add ...`.

### Configuration (env vars)

| Var | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | Listen port |
| `DB_PATH` | `/data/club.db` | SQLite file (mount a volume!) |
| `COOKIE_SECRET` | (required) | Signs the "who am I" cookie |
| `TIMEZONE` | `Europe/London` | Deadlines are interpreted in this zone |
| `YTMUSIC_HEADERS_JSON` | (unset) | Enables YouTube Music link matching: the contents of `ytmusic_headers.json` (§5.5). Secret. |
| `LINK_CACHE_TTL` | `168h` | How long provider search responses are cached |

Link matching is enabled only when the headers are present, and the container needs outbound HTTPS for it. Pass keys via env vars or Docker secrets, never bake them into the image.

Window lengths (`submission_days`, `voting_days`) live on the `clubs` row, so each group can differ.

### Backups

The whole state is one file. Simplest: a host cron running `sqlite3 club.db ".backup /backups/club-$(date +%F).db"`, or Litestream later for continuous replication.

## 13. Assumptions to Confirm

These are my interpretations of the rules; each is a small, local change if wrong.

1. **Own-song votes:** a player cannot vote for their own song in either category.
2. **Category bonus:** +1 *per category won*, so a song winning both categories earns +2 (a single "+1 for winning either" would be a one-line change in `Rules`/`categoryWinners`).
3. **Overall winner:** favourite votes + best-fit votes combined. It is a stat/honour only and earns no extra points.
4. **Duplicates in voting:** duplicate songs appear as separate entries and are voted on and scored independently; each entry's submitter gets the votes cast for it.
5. **Featured quotes:** taken from the voters' comments on the winning songs (the favourite comment for the Best Song winner, the best-fit comment for the Best Theme winner). If you meant the submitter's own `note` as well, it is trivial to offer it as another candidate in the picker.
6. **`review` phase:** introduced so the admin can curate quotes and fix duplicates *before* the reveal. Results stay hidden until the admin publishes.
7. **Duplicate detection** keys on normalised artist + title (not URL), with an admin override.
8. **YouTube Music:** since no official API exists, matching uses the unofficial `ytmusicapi` client (§5.5). It can break without notice; if it does, submitting by pasted link still works.
9. **Other services** (Spotify, Qobuz, ...) are not matched; their pasted links are classified but never searched.
10. **Matched metadata overwrites what the player typed** (artist/title), which improves duplicate detection. Manual URLs keep the player's own text.
11. **Manual URLs** may point anywhere (Bandcamp, SoundCloud, ...) and are never fetched or verified.
12. **One link per song:** the one the player confirmed or pasted.

## 14. Testing Strategy

- **`scoring`:** table-driven unit tests; the most valuable tests in the project. Cases: simple round, tied winners, zero votes, both-category winner, 2- and 3-way duplicates, trendsetter order after a key-changing edit.
- **`songkey`:** table of equivalent/non-equivalent titles (`feat.`, remasters, punctuation, accents).
- **`linkresolver`:** the resolver is tested with a fake `Provider` (cache hit and expiry, the two-result cap, errors not cached, no provider); the YouTube Music client's result mapping and header normalisation are pure functions tested directly; URL validation covers scheme, `javascript:` rejection and tracking params. Handlers are tested with a fake `Provider`, so no test touches the network.
- **`rounds`:** phase-transition tests with an injected clock against in-memory SQLite, including the full-assignment validator and the review→publish flow.
- **`store`:** integration tests on `:memory:` SQLite with migrations applied; include a **two-club test** asserting no data leaks between clubs.
- **`web`:** a few `httptest` smoke tests per route (renders, rejects wrong-phase writes, hides results before `closed`, rejects non-admins).

## 15. Suggested Build Order

1. Skeleton: `main.go`, config, DB open + migrations, `/healthz`, Dockerfile, `club add` command.
2. Club middleware, `/c/{club}` routing, `whoami` cookie, admin player management.
3. Admin kickoff, picker selection, theme form.
4. Submission form (manual URL only at first, with `songkey` and URL validation) and the submitting-phase page.
5. `Tick` and the scheduler (`submitting → voting → review`).
6. Ballot form with guess assignment (HTMX disabling of used players).
7. `scoring` package + tests, then admin review page, quote picker and `Publish`.
8. Results page, then monthly and cumulative stats.
9. Link matching (can ship independently, since the manual-URL form already works): `linkresolver` with the YouTube Music provider, candidate-card fragment, "paste my own URL" fallback and `link_cache`.
10. Polish: countdowns, polling fragments, empty states.
11. Later: more link providers (Spotify, Qobuz), real auth, embedded players, notifications (email/webhook when a phase opens), a site-admin page for creating clubs.
