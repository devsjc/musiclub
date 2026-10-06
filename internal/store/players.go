package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"musiclub/internal/domain"
)

const maxPlayerName = 50

var (
	ErrPlayerNameRequired = errors.New("player name is required")
	ErrPlayerNameTooLong  = fmt.Errorf("player name must be at most %d characters", maxPlayerName)
	ErrPlayerNameTaken    = errors.New("a player with that name already exists in this club")
	ErrLastAdmin          = errors.New("a club must keep at least one active admin")
)

const playerCols = `id, club_id, name, active, is_admin, created_at`

type Players struct{ db *sql.DB }

func NewPlayers(db *sql.DB) *Players { return &Players{db: db} }

// Create adds an active player to the club. The name is stored in lower case,
// so names are unique regardless of the case they were typed in.
func (s *Players) Create(ctx context.Context, clubID int64, name string, isAdmin bool) (domain.Player, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return domain.Player{}, ErrPlayerNameRequired
	}
	if utf8.RuneCountInString(name) > maxPlayerName {
		return domain.Player{}, ErrPlayerNameTooLong
	}

	res, err := s.db.ExecContext(ctx,
		`INSERT INTO players (club_id, name, is_admin) VALUES (?, ?, ?)`, clubID, name, isAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: players.club_id, players.name") {
			return domain.Player{}, ErrPlayerNameTaken
		}
		return domain.Player{}, fmt.Errorf("insert player: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return domain.Player{}, err
	}
	return s.Get(ctx, clubID, id)
}

// Get returns the player if it belongs to the club, otherwise ErrNotFound.
func (s *Players) Get(ctx context.Context, clubID, id int64) (domain.Player, error) {
	return getPlayer(ctx, s.db, clubID, id)
}

// List returns every player in the club (active or not) ordered by name.
func (s *Players) List(ctx context.Context, clubID int64) ([]domain.Player, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+playerCols+` FROM players WHERE club_id = ? ORDER BY name COLLATE NOCASE`, clubID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Player
	for rows.Next() {
		p, err := scanPlayer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetActive retires or reinstates a player. It refuses to retire the last
// active admin.
func (s *Players) SetActive(ctx context.Context, clubID, id int64, active bool) error {
	return s.mutate(ctx, clubID, id, func(p *domain.Player) { p.Active = active })
}

// SetAdmin grants or revokes admin. It refuses to revoke the last active admin.
func (s *Players) SetAdmin(ctx context.Context, clubID, id int64, admin bool) error {
	return s.mutate(ctx, clubID, id, func(p *domain.Player) { p.IsAdmin = admin })
}

func (s *Players) mutate(ctx context.Context, clubID, id int64, change func(*domain.Player)) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	p, err := getPlayer(ctx, tx, clubID, id)
	if err != nil {
		return err
	}
	wasAdmin := p.Active && p.IsAdmin
	change(&p)

	if wasAdmin && !(p.Active && p.IsAdmin) {
		var others int
		if err := tx.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM players WHERE club_id = ? AND active = 1 AND is_admin = 1 AND id <> ?`,
			clubID, id).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return ErrLastAdmin
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE players SET active = ?, is_admin = ? WHERE id = ? AND club_id = ?`,
		p.Active, p.IsAdmin, id, clubID); err != nil {
		return err
	}
	return tx.Commit()
}

type queryRower interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getPlayer(ctx context.Context, q queryRower, clubID, id int64) (domain.Player, error) {
	p, err := scanPlayer(q.QueryRowContext(ctx,
		`SELECT `+playerCols+` FROM players WHERE club_id = ? AND id = ?`, clubID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Player{}, ErrNotFound
	}
	return p, err
}

func scanPlayer(r interface{ Scan(...any) error }) (domain.Player, error) {
	var p domain.Player
	var created string
	if err := r.Scan(&p.ID, &p.ClubID, &p.Name, &p.Active, &p.IsAdmin, &created); err != nil {
		return domain.Player{}, err
	}
	t, err := parseTime(created)
	if err != nil {
		return domain.Player{}, err
	}
	p.CreatedAt = t
	return p, nil
}
