package store

import (
	"context"
	"time"

	"musiclub/internal/domain"
)

// AdvancePhases checks all rounds in 'submitting' or 'voting' and advances them
// if their deadlines have passed or all active players have submitted/voted.
func (s *Rounds) AdvancePhases(ctx context.Context, now time.Time, shuffle func(n int, swap func(i, j int))) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+roundCols+` FROM rounds WHERE phase IN ('submitting', 'voting')`)
	if err != nil {
		return err
	}
	defer rows.Close()

	var rounds []domain.Round
	for rows.Next() {
		r, err := scanRound(rows)
		if err != nil {
			return err
		}
		rounds = append(rounds, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, r := range rounds {
		if err := s.advanceOne(ctx, r, now, shuffle); err != nil {
			return err
		}
	}
	return nil
}

func (s *Rounds) advanceOne(ctx context.Context, r domain.Round, now time.Time, shuffle func(n int, swap func(i, j int))) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Re-check phase in case of race
	var phase string
	if err := tx.QueryRowContext(ctx, `SELECT phase FROM rounds WHERE id = ?`, r.ID).Scan(&phase); err != nil {
		return err
	}
	if phase != string(r.Phase) {
		return nil // changed by another Tick or request
	}

	if r.Phase == domain.PhaseSubmitting {
		if r.SubmissionsDueAt == nil {
			return nil
		}
		var missing int
		err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM players WHERE club_id = ? AND active = 1 AND id NOT IN (SELECT player_id FROM submissions WHERE round_id = ?)`,
			r.ClubID, r.ID).Scan(&missing)
		if err != nil {
			return err
		}

		if missing > 0 && r.SubmissionsDueAt.After(now) {
			return nil
		}

		// Advance to voting
		if _, err := tx.ExecContext(ctx,
			`UPDATE rounds SET phase = 'voting',
			    voting_due_at = datetime(?, '+' || (SELECT voting_days FROM clubs WHERE id = rounds.club_id) || ' days')
			 WHERE id = ?`, now.UTC().Format(sqliteTime), r.ID); err != nil {
			return err
		}

		// Assign random positions to submissions
		subRows, err := tx.QueryContext(ctx, `SELECT id FROM submissions WHERE round_id = ?`, r.ID)
		if err != nil {
			return err
		}
		defer subRows.Close()
		var subIDs []int64
		for subRows.Next() {
			var id int64
			if err := subRows.Scan(&id); err != nil {
				return err
			}
			subIDs = append(subIDs, id)
		}
		if err := subRows.Err(); err != nil {
			return err
		}

		shuffle(len(subIDs), func(i, j int) { subIDs[i], subIDs[j] = subIDs[j], subIDs[i] })

		for i, id := range subIDs {
			if _, err := tx.ExecContext(ctx, `UPDATE submissions SET position = ? WHERE id = ?`, i+1, id); err != nil {
				return err
			}
		}

		return tx.Commit()
	}

	if r.Phase == domain.PhaseVoting {
		if r.VotingDueAt == nil {
			return nil
		}
		var missing int
		err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM players WHERE club_id = ? AND active = 1 AND id NOT IN (SELECT voter_id FROM ballots WHERE round_id = ?)`,
			r.ClubID, r.ID).Scan(&missing)
		if err != nil {
			return err
		}

		if missing > 0 && r.VotingDueAt.After(now) {
			return nil
		}

		// Advance to review
		if _, err := tx.ExecContext(ctx, `UPDATE rounds SET phase = 'review' WHERE id = ?`, r.ID); err != nil {
			return err
		}
		return tx.Commit()
	}

	return nil
}
