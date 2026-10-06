package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"musiclub/internal/domain"
)

var ErrAlreadyVoted = errors.New("you have already cast your ballot")

type Ballots struct{ db *sql.DB }

func NewBallots(db *sql.DB) *Ballots { return &Ballots{db: db} }

// Create saves a voter's ballot and guesses. The round must belong to the club
// and be in the voting phase with its deadline after now. The voter must be active.
func (s *Ballots) Create(ctx context.Context, clubID int64, b domain.Ballot, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	r, err := getRound(ctx, tx, `club_id = ? AND id = ?`, clubID, b.RoundID)
	if err != nil {
		return err
	}
	if r.Phase != domain.PhaseVoting {
		return ErrWrongPhase
	}
	if r.VotingDueAt == nil || !now.UTC().Before(*r.VotingDueAt) {
		return ErrDeadlinePassed
	}
	var active int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM players WHERE club_id = ? AND id = ? AND active = 1`, clubID, b.VoterID).Scan(&active); err != nil {
		return err
	}
	if active == 0 {
		return ErrNotFound
	}
	var existing int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ballots WHERE round_id = ? AND voter_id = ?`, b.RoundID, b.VoterID).Scan(&existing)
	if err != nil {
		return err
	}
	if existing > 0 {
		return ErrAlreadyVoted
	}

	stamp := now.UTC().Format(sqliteTime)
	res, err := tx.ExecContext(ctx, `
		INSERT INTO ballots (round_id, voter_id, favorite_submission_id, favorite_comment, best_fit_submission_id, best_fit_comment, submitted_at)
		VALUES (?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?)
	`, b.RoundID, b.VoterID, b.FavoriteSubmissionID, b.FavoriteComment, b.BestFitSubmissionID, b.BestFitComment, stamp)
	if err != nil {
		return fmt.Errorf("insert ballot: %w", err)
	}
	ballotID, err := res.LastInsertId()
	if err != nil {
		return err
	}

	for _, g := range b.Guesses {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO guesses (ballot_id, submission_id, guessed_player_id)
			VALUES (?, ?, ?)
		`, ballotID, g.SubmissionID, g.GuessedPlayerID)
		if err != nil {
			return fmt.Errorf("insert guess: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Ballots) ByVoter(ctx context.Context, clubID, roundID, voterID int64) (domain.Ballot, error) {
	var b domain.Ballot
	var at string
	err := s.db.QueryRowContext(ctx, `
		SELECT b.id, b.round_id, b.voter_id, b.favorite_submission_id, coalesce(b.favorite_comment, ''), b.best_fit_submission_id, coalesce(b.best_fit_comment, ''), b.submitted_at
		FROM ballots b
		JOIN rounds r ON r.id = b.round_id
		WHERE r.club_id = ? AND b.round_id = ? AND b.voter_id = ?
	`, clubID, roundID, voterID).Scan(&b.ID, &b.RoundID, &b.VoterID, &b.FavoriteSubmissionID, &b.FavoriteComment, &b.BestFitSubmissionID, &b.BestFitComment, &at)
	if err == sql.ErrNoRows {
		return domain.Ballot{}, ErrNotFound
	}
	if err != nil {
		return domain.Ballot{}, err
	}
	if b.SubmittedAt, err = parseTime(at); err != nil {
		return domain.Ballot{}, err
	}

	rows, err := s.db.QueryContext(ctx, `SELECT submission_id, guessed_player_id FROM guesses WHERE ballot_id = ?`, b.ID)
	if err != nil {
		return domain.Ballot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var g domain.Guess
		if err := rows.Scan(&g.SubmissionID, &g.GuessedPlayerID); err != nil {
			return domain.Ballot{}, err
		}
		b.Guesses = append(b.Guesses, g)
	}
	return b, rows.Err()
}

// Voters returns the set of player IDs who have cast a ballot in the round.
func (s *Ballots) Voters(ctx context.Context, clubID, roundID int64) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT b.voter_id FROM ballots b JOIN rounds r ON r.id = b.round_id
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
