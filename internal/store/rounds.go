package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"musiclub/internal/domain"
)

var (
	ErrRoundOpen     = errors.New("this club already has a round in progress")
	ErrMonthTaken    = errors.New("this club already has a round for that month")
	ErrNoPlayers     = errors.New("the club has no active players to pick a theme")
	ErrNoOtherPicker = errors.New("there is no other active player who could pick the theme")
	ErrWrongPhase    = errors.New("the round is not in the right phase for that")
	ErrNotPicker     = errors.New("only the picker can set the theme")
)

const roundCols = `id, club_id, month, cycle, picker_id, COALESCE(theme, ''),
	phase, submissions_due_at, voting_due_at, created_at, closed_at`

// ChoosePicker is given the IDs of the eligible players (never empty) and
// returns the one to use. Keeping the random choice outside the store lets the
// rounds service own it and tests make it deterministic.
type ChoosePicker func(candidateIDs []int64) int64

type Rounds struct{ db *sql.DB }

func NewRounds(db *sql.DB) *Rounds { return &Rounds{db: db} }

// Create inserts a theme_pending round for the month, with a picker chosen
// from the active players who haven't picked in the club's current cycle
// (DESIGN.md §5.4). If everyone has picked, a new cycle starts. A club can
// only have one round that isn't closed.
func (s *Rounds) Create(ctx context.Context, clubID int64, month string, choose ChoosePicker) (domain.Round, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Round{}, err
	}
	defer tx.Rollback()

	var open int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM rounds WHERE club_id = ? AND phase <> 'closed'`, clubID).Scan(&open); err != nil {
		return domain.Round{}, err
	}
	if open > 0 {
		return domain.Round{}, ErrRoundOpen
	}

	cycle := 1
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(cycle), 1) FROM rounds WHERE club_id = ?`, clubID).Scan(&cycle); err != nil {
		return domain.Round{}, err
	}
	ids, err := unpicked(ctx, tx, clubID, cycle, 0, 0)
	if err != nil {
		return domain.Round{}, err
	}
	if len(ids) == 0 {
		cycle++
		if ids, err = unpicked(ctx, tx, clubID, cycle, 0, 0); err != nil {
			return domain.Round{}, err
		}
	}
	if len(ids) == 0 {
		return domain.Round{}, ErrNoPlayers
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO rounds (club_id, month, cycle, picker_id) VALUES (?, ?, ?, ?)`,
		clubID, month, cycle, choose(ids))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: rounds.club_id, rounds.month") {
			return domain.Round{}, ErrMonthTaken
		}
		return domain.Round{}, fmt.Errorf("insert round: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Round{}, err
	}
	r, err := getRound(ctx, tx, `club_id = ? AND id = ?`, clubID, id)
	if err != nil {
		return domain.Round{}, err
	}
	return r, tx.Commit()
}

// Reroll replaces the picker of a theme_pending round with a different active
// player who hasn't picked in the round's cycle. If nobody else is left in the
// cycle, the round moves to the next cycle and anyone but the current picker
// is eligible.
func (s *Rounds) Reroll(ctx context.Context, clubID, roundID int64, choose ChoosePicker) (domain.Round, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Round{}, err
	}
	defer tx.Rollback()

	r, err := getRound(ctx, tx, `club_id = ? AND id = ?`, clubID, roundID)
	if err != nil {
		return domain.Round{}, err
	}
	if r.Phase != domain.PhaseThemePending {
		return domain.Round{}, ErrWrongPhase
	}

	cycle := r.Cycle
	ids, err := unpicked(ctx, tx, clubID, cycle, r.ID, r.PickerID)
	if err != nil {
		return domain.Round{}, err
	}
	if len(ids) == 0 {
		cycle++
		if ids, err = unpicked(ctx, tx, clubID, cycle, r.ID, r.PickerID); err != nil {
			return domain.Round{}, err
		}
	}
	if len(ids) == 0 {
		return domain.Round{}, ErrNoOtherPicker
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE rounds SET picker_id = ?, cycle = ? WHERE id = ? AND club_id = ?`,
		choose(ids), cycle, r.ID, clubID); err != nil {
		return domain.Round{}, err
	}
	r, err = getRound(ctx, tx, `club_id = ? AND id = ?`, clubID, roundID)
	if err != nil {
		return domain.Round{}, err
	}
	return r, tx.Commit()
}

// SetTheme stores the theme and opens the submission window: the round moves
// to submitting with a deadline of now plus the club's submission_days. Only
// the round's picker may do this, and only while it is theme_pending.
func (s *Rounds) SetTheme(ctx context.Context, clubID, roundID, pickerID int64, theme string, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE rounds
		    SET theme = ?, phase = 'submitting',
		        submissions_due_at = datetime(?, '+' || (SELECT submission_days FROM clubs WHERE id = rounds.club_id) || ' days')
		  WHERE club_id = ? AND id = ? AND picker_id = ? AND phase = 'theme_pending'`,
		theme, now.UTC().Format(sqliteTime), clubID, roundID, pickerID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n > 0 {
		return err
	}

	// Nothing updated: work out why.
	r, err := s.Get(ctx, clubID, roundID)
	if err != nil {
		return err
	}
	if r.Phase != domain.PhaseThemePending {
		return ErrWrongPhase
	}
	return ErrNotPicker
}

// Get returns the round if it belongs to the club, otherwise ErrNotFound.
func (s *Rounds) Get(ctx context.Context, clubID, id int64) (domain.Round, error) {
	return getRound(ctx, s.db, `club_id = ? AND id = ?`, clubID, id)
}

// ByMonth returns the club's round for a "YYYY-MM" month, or ErrNotFound.
func (s *Rounds) ByMonth(ctx context.Context, clubID int64, month string) (domain.Round, error) {
	return getRound(ctx, s.db, `club_id = ? AND month = ?`, clubID, month)
}

// Current returns the club's round that isn't closed yet, or ErrNotFound.
func (s *Rounds) Current(ctx context.Context, clubID int64) (domain.Round, error) {
	return getRound(ctx, s.db, `club_id = ? AND phase <> 'closed'`, clubID)
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// unpicked returns the IDs of the club's active players who haven't picked in
// the cycle, ignoring the round ignoreRound (whose picker is being replaced)
// and the player exclude. Zero means "none".
func unpicked(ctx context.Context, q queryer, clubID int64, cycle int, ignoreRound, exclude int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT id FROM players
		  WHERE club_id = ? AND active = 1 AND id <> ?
		    AND id NOT IN (SELECT picker_id FROM rounds WHERE club_id = ? AND cycle = ? AND id <> ?)
		  ORDER BY id`,
		clubID, exclude, clubID, cycle, ignoreRound)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func getRound(ctx context.Context, q queryer, where string, args ...any) (domain.Round, error) {
	r, err := scanRound(q.QueryRowContext(ctx, `SELECT `+roundCols+` FROM rounds WHERE `+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Round{}, ErrNotFound
	}
	return r, err
}

func scanRound(row interface{ Scan(...any) error }) (domain.Round, error) {
	var r domain.Round
	var phase, created string
	var subDue, voteDue, closed sql.NullString
	if err := row.Scan(&r.ID, &r.ClubID, &r.Month, &r.Cycle, &r.PickerID, &r.Theme,
		&phase, &subDue, &voteDue, &created, &closed); err != nil {
		return domain.Round{}, err
	}
	r.Phase = domain.Phase(phase)

	var err error
	if r.CreatedAt, err = parseTime(created); err != nil {
		return domain.Round{}, err
	}
	for _, f := range []struct {
		in  sql.NullString
		out **time.Time
	}{{subDue, &r.SubmissionsDueAt}, {voteDue, &r.VotingDueAt}, {closed, &r.ClosedAt}} {
		if !f.in.Valid {
			continue
		}
		t, err := parseTime(f.in.String)
		if err != nil {
			return domain.Round{}, err
		}
		*f.out = &t
	}
	return r, nil
}
