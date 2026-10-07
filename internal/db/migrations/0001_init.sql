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
    submitted_at  TEXT    NOT NULL DEFAULT (datetime('now')),  -- reset only when song_key changes
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
