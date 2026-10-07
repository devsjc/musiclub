package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"musiclub/internal/domain"
)

var (
	ErrDeadlinePassed   = errors.New("the submission deadline has passed")
	ErrAlreadySubmitted = errors.New("you have already submitted a song for this round")
)

const submissionCols = `id, round_id, player_id, title, artist, url, url_service, link_source,
	COALESCE(external_id, ''), COALESCE(note, ''), song_key, submitted_at, position`

type Submissions struct{ db *sql.DB }

func NewSubmissions(db *sql.DB) *Submissions { return &Submissions{db: db} }

// Song is what a player gives when submitting. SongKey is computed by the caller.
type Song struct {
	Title, Artist string
	URL           string
	URLService    string
	LinkSource    string
	ExternalID    string
	Note          string
	SongKey       string
}

// Create saves the player's song for the round. The round must
// belong to the club and be in the submitting phase with its deadline after now, and the
// player must be an active member of the club. If a submission already exists,
// it returns ErrAlreadySubmitted.
func (s *Submissions) Create(ctx context.Context, clubID, roundID, playerID int64, song Song, now time.Time) (domain.Submission, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Submission{}, err
	}
	defer tx.Rollback()

	r, err := getRound(ctx, tx, `club_id = ? AND id = ?`, clubID, roundID)
	if err != nil {
		return domain.Submission{}, err
	}
	if r.Phase != domain.PhaseSubmitting {
		return domain.Submission{}, ErrWrongPhase
	}
	if r.SubmissionsDueAt == nil || !now.UTC().Before(*r.SubmissionsDueAt) {
		return domain.Submission{}, ErrDeadlinePassed
	}
	var active int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM players WHERE club_id = ? AND id = ? AND active = 1`, clubID, playerID).Scan(&active); err != nil {
		return domain.Submission{}, err
	}
	if active == 0 {
		return domain.Submission{}, ErrNotFound
	}

	var existing int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM submissions WHERE round_id = ? AND player_id = ?`, roundID, playerID).Scan(&existing)
	if err != nil {
		return domain.Submission{}, err
	}
	if existing > 0 {
		return domain.Submission{}, ErrAlreadySubmitted
	}

	stamp := now.UTC().Format(sqliteTime)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO submissions (round_id, player_id, title, artist, url, url_service, link_source, external_id, note, song_key, submitted_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		roundID, playerID, song.Title, song.Artist, song.URL, song.URLService, song.LinkSource, song.ExternalID, song.Note, song.SongKey, stamp)
	if err != nil {
		return domain.Submission{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Submission{}, err
	}

	sub, err := scanSubmission(tx.QueryRowContext(ctx, `SELECT `+submissionCols+` FROM submissions WHERE id = ?`, id))
	if err != nil {
		return domain.Submission{}, err
	}
	return sub, tx.Commit()
}

// List returns all submissions for the round, ordered by position (or id).
func (s *Submissions) List(ctx context.Context, clubID, roundID int64) ([]domain.Submission, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+submissionCols+` FROM submissions
		  WHERE round_id = ? AND round_id IN (SELECT id FROM rounds WHERE id = ? AND club_id = ?)
		  ORDER BY COALESCE(position, id), id`,
		roundID, roundID, clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Submission
	for rows.Next() {
		sub, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch extra links
	if len(out) > 0 {
		linkRows, err := s.db.QueryContext(ctx,
			`SELECT submission_id, service, url FROM submission_links
			  WHERE submission_id IN (SELECT id FROM submissions WHERE round_id = ?)
			  ORDER BY service`,
			roundID)
		if err == nil {
			defer linkRows.Close()
			linksBySub := make(map[int64][]domain.SubmissionLink)
			for linkRows.Next() {
				var l domain.SubmissionLink
				if err := linkRows.Scan(&l.SubmissionID, &l.Service, &l.URL); err == nil {
					linksBySub[l.SubmissionID] = append(linksBySub[l.SubmissionID], l)
				}
			}
			for i, sub := range out {
				if links, ok := linksBySub[sub.ID]; ok {
					out[i].ExtraLinks = links
				}
			}
		}
	}

	return out, nil
}

// Delete removes a submission. Only an admin can invoke the corresponding route.
func (s *Submissions) Delete(ctx context.Context, clubID, roundID, submissionID int64) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM submissions 
		 WHERE id = ? AND round_id IN (SELECT id FROM rounds WHERE id = ? AND club_id = ?)`,
		submissionID, roundID, clubID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ByPlayer returns the player's song for the round, or ErrNotFound. The round
// must belong to the club.
func (s *Submissions) ByPlayer(ctx context.Context, clubID, roundID, playerID int64) (domain.Submission, error) {
	sub, err := scanSubmission(s.db.QueryRowContext(ctx,
		`SELECT `+submissionCols+` FROM submissions
		  WHERE round_id = ? AND player_id = ?
		    AND round_id IN (SELECT id FROM rounds WHERE id = ? AND club_id = ?)`,
		roundID, playerID, roundID, clubID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Submission{}, ErrNotFound
	}
	return sub, err
}

// Submitters returns the IDs of the players who have submitted a song to the
// round. It says who, never what: that stays hidden until the round is closed.
func (s *Submissions) Submitters(ctx context.Context, clubID, roundID int64) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT s.player_id FROM submissions s JOIN rounds r ON r.id = s.round_id
		  WHERE r.club_id = ? AND r.id = ?`, clubID, roundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

func (s *Submissions) SetSongKey(ctx context.Context, clubID, roundID, submissionID int64, key string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE submissions SET song_key = ?
		 WHERE id = ? AND round_id IN (SELECT id FROM rounds WHERE id = ? AND club_id = ?)`,
		key, submissionID, roundID, clubID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanSubmission(row interface{ Scan(...any) error }) (domain.Submission, error) {
	var sub domain.Submission
	var at string
	var pos sql.NullInt64
	if err := row.Scan(&sub.ID, &sub.RoundID, &sub.PlayerID, &sub.Title, &sub.Artist, &sub.URL, &sub.URLService,
		&sub.LinkSource, &sub.ExternalID, &sub.Note, &sub.SongKey, &at, &pos); err != nil {
		return domain.Submission{}, err
	}
	var err error
	if sub.SubmittedAt, err = parseTime(at); err != nil {
		return domain.Submission{}, err
	}
	if pos.Valid {
		p := int(pos.Int64)
		sub.Position = &p
	}
	return sub, nil
}
