package store

import (
	"context"
	"time"

	"musiclub/internal/domain"
)

type Scores struct{ db queryer }

// PublishRound writes score events and closes the round in a single transaction.
func (s *Rounds) Publish(ctx context.Context, clubID, roundID int64, events []domain.ScoreEvent, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Ensure round belongs to club and is in review phase
	var phase string
	err = tx.QueryRowContext(ctx, `SELECT phase FROM rounds WHERE id = ? AND club_id = ?`, roundID, clubID).Scan(&phase)
	if err != nil {
		return err
	}
	if phase != string(domain.PhaseReview) {
		return ErrWrongPhase
	}

	// Delete existing events (in case of recompute later, though this is initial publish)
	_, err = tx.ExecContext(ctx, `DELETE FROM score_events WHERE round_id = ?`, roundID)
	if err != nil {
		return err
	}

	// Insert events
	for _, e := range events {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO score_events (round_id, player_id, kind, points, submission_id, ballot_id)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			roundID, e.PlayerID, e.Kind, e.Points, e.SubmissionID, e.BallotID)
		if err != nil {
			return err
		}
	}

	// Close round
	stamp := now.UTC().Format(sqliteTime)
	_, err = tx.ExecContext(ctx,
		`UPDATE rounds SET phase = 'closed', closed_at = ? WHERE id = ?`,
		stamp, roundID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Recompute writes score events for a closed round.
func (s *Rounds) Recompute(ctx context.Context, clubID, roundID int64, events []domain.ScoreEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Ensure round belongs to club and is closed
	var phase string
	err = tx.QueryRowContext(ctx, `SELECT phase FROM rounds WHERE id = ? AND club_id = ?`, roundID, clubID).Scan(&phase)
	if err != nil {
		return err
	}
	if phase != string(domain.PhaseClosed) {
		return ErrWrongPhase
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM score_events WHERE round_id = ?`, roundID)
	if err != nil {
		return err
	}

	for _, e := range events {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO score_events (round_id, player_id, kind, points, submission_id, ballot_id)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			roundID, e.PlayerID, e.Kind, e.Points, e.SubmissionID, e.BallotID)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
