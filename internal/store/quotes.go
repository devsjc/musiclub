package store

import (
	"context"
	"database/sql"
	"musiclub/internal/domain"
)

type Quotes struct{ db *sql.DB }

func NewQuotes(db *sql.DB) *Quotes { return &Quotes{db: db} }

// Set replaces all featured quotes for the round.
func (q *Quotes) Set(ctx context.Context, clubID, roundID int64, quotes []domain.FeaturedQuote) error {
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Verify round belongs to club
	var exists bool
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM rounds WHERE id = ? AND club_id = ?`, roundID, clubID).Scan(&exists)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM featured_quotes WHERE round_id = ?`, roundID)
	if err != nil {
		return err
	}

	for i, quote := range quotes {
		var ballotID *int64
		if quote.BallotID != nil && *quote.BallotID > 0 {
			ballotID = quote.BallotID
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO featured_quotes (round_id, category, submission_id, ballot_id, quote, position)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			roundID, quote.Category, quote.SubmissionID, ballotID, quote.Quote, i)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (q *Quotes) List(ctx context.Context, clubID, roundID int64) ([]domain.FeaturedQuote, error) {
	rows, err := q.db.QueryContext(ctx,
		`SELECT q.id, q.round_id, q.category, q.submission_id, q.ballot_id, q.quote, q.position
		 FROM featured_quotes q
		 JOIN rounds r ON r.id = q.round_id
		 WHERE q.round_id = ? AND r.club_id = ?
		 ORDER BY q.category, q.position`, roundID, clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.FeaturedQuote
	for rows.Next() {
		var fq domain.FeaturedQuote
		var bid sql.NullInt64
		if err := rows.Scan(&fq.ID, &fq.RoundID, &fq.Category, &fq.SubmissionID, &bid, &fq.Quote, &fq.Position); err != nil {
			return nil, err
		}
		if bid.Valid {
			id := bid.Int64
			fq.BallotID = &id
		}
		out = append(out, fq)
	}
	return out, rows.Err()
}
