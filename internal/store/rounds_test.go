package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/db"
	"musiclub/internal/domain"
)

type roundsEnv struct {
	rounds  *Rounds
	players *Players
	club    domain.Club
	other   domain.Club
}

func newRounds(t *testing.T) *roundsEnv {
	t.Helper()
	d, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })

	clubs := NewClubs(d)
	a, err := clubs.Create(context.Background(), "a", "A")
	require.NoError(t, err)
	b, err := clubs.Create(context.Background(), "b", "B")
	require.NoError(t, err)
	return &roundsEnv{rounds: NewRounds(d), players: NewPlayers(d), club: a, other: b}
}

func (e *roundsEnv) player(t *testing.T, c domain.Club, name string) domain.Player {
	t.Helper()
	p, err := e.players.Create(context.Background(), c.ID, name, false)
	require.NoError(t, err)
	return p
}

// first always picks the lowest ID, last the highest.
func first(ids []int64) int64 { return ids[0] }
func last(ids []int64) int64  { return ids[len(ids)-1] }

func (e *roundsEnv) closeRound(t *testing.T, r domain.Round) {
	t.Helper()
	_, err := e.rounds.db.Exec(`UPDATE rounds SET phase = 'closed' WHERE id = ?`, r.ID)
	require.NoError(t, err)
}

func TestRoundsCreate(t *testing.T) {
	e := newRounds(t)
	ctx := context.Background()
	sam := e.player(t, e.club, "sam")

	r, err := e.rounds.Create(ctx, e.club.ID, "2026-03", first)
	require.NoError(t, err)
	require.Equal(t, sam.ID, r.PickerID)
	require.Equal(t, 1, r.Cycle)
	require.Equal(t, domain.PhaseThemePending, r.Phase)
	require.Empty(t, r.Theme)
	require.Nil(t, r.SubmissionsDueAt)
	require.Equal(t, e.club.ID, r.ClubID)

	_, err = e.rounds.Create(ctx, e.club.ID, "2026-04", first)
	require.ErrorIs(t, err, ErrRoundOpen, "second open round")
	e.closeRound(t, r)
	_, err = e.rounds.Create(ctx, e.club.ID, "2026-03", first)
	require.ErrorIs(t, err, ErrMonthTaken, "repeat month")
}

func TestRoundsCreateNeedsActivePlayers(t *testing.T) {
	e := newRounds(t)
	ctx := context.Background()
	_, err := e.rounds.Create(ctx, e.club.ID, "2026-03", first)
	require.ErrorIs(t, err, ErrNoPlayers, "no players")

	retired := e.player(t, e.club, "retired")
	e.player(t, e.club, "admin")
	_, err = e.players.db.Exec(`UPDATE players SET active = 0 WHERE id = ?`, retired.ID)
	require.NoError(t, err)
	r, err := e.rounds.Create(ctx, e.club.ID, "2026-03", last)
	require.NoError(t, err)
	require.NotEqual(t, retired.ID, r.PickerID, "retired player must not be picked")
}

func TestRoundsPickerCycle(t *testing.T) {
	e := newRounds(t)
	ctx := context.Background()
	a, b, c := e.player(t, e.club, "a"), e.player(t, e.club, "b"), e.player(t, e.club, "c")

	var picked []int64
	var cycles []int
	for _, month := range []string{"2026-01", "2026-02", "2026-03", "2026-04"} {
		r, err := e.rounds.Create(ctx, e.club.ID, month, first)
		require.NoError(t, err)
		picked, cycles = append(picked, r.PickerID), append(cycles, r.Cycle)
		e.closeRound(t, r)
	}
	// Nobody repeats within a cycle; once everyone has picked, cycle 2 starts.
	require.Equal(t, []int64{a.ID, b.ID, c.ID, a.ID}, picked)
	require.Equal(t, []int{1, 1, 1, 2}, cycles)
}

func TestRoundsPickerCycleSkipsPlayersWhoJoinLate(t *testing.T) {
	e := newRounds(t)
	ctx := context.Background()
	a := e.player(t, e.club, "a")
	r, err := e.rounds.Create(ctx, e.club.ID, "2026-01", first)
	require.NoError(t, err)
	require.Equal(t, a.ID, r.PickerID)
	e.closeRound(t, r)

	b := e.player(t, e.club, "b") // hasn't picked in cycle 1 yet
	r, err = e.rounds.Create(ctx, e.club.ID, "2026-02", first)
	require.NoError(t, err)
	require.Equal(t, b.ID, r.PickerID, "want b")
	require.Equal(t, 1, r.Cycle)
}

func TestRoundsReroll(t *testing.T) {
	e := newRounds(t)
	ctx := context.Background()
	a, b, c := e.player(t, e.club, "a"), e.player(t, e.club, "b"), e.player(t, e.club, "c")

	r, err := e.rounds.Create(ctx, e.club.ID, "2026-01", first)
	require.NoError(t, err)
	require.Equal(t, a.ID, r.PickerID)
	r, err = e.rounds.Reroll(ctx, e.club.ID, r.ID, first)
	require.NoError(t, err)
	require.Equal(t, b.ID, r.PickerID, "never the current picker")
	// a was released back into the cycle, so a can come up again.
	r, err = e.rounds.Reroll(ctx, e.club.ID, r.ID, first)
	require.NoError(t, err)
	require.Equal(t, a.ID, r.PickerID)
	// Once the closed round has used up b and c, rerolling rolls over to the next cycle.
	e.closeRound(t, r)
	r2, err := e.rounds.Create(ctx, e.club.ID, "2026-02", last) // c
	require.NoError(t, err)
	require.Equal(t, c.ID, r2.PickerID)
	e.closeRound(t, r2)
	r3, err := e.rounds.Create(ctx, e.club.ID, "2026-03", first) // b: the only one left in cycle 1
	require.NoError(t, err)
	require.Equal(t, b.ID, r3.PickerID)
	require.Equal(t, 1, r3.Cycle)
	r3, err = e.rounds.Reroll(ctx, e.club.ID, r3.ID, first)
	require.NoError(t, err)
	require.NotEqual(t, b.ID, r3.PickerID, "want someone else")
	require.Equal(t, 2, r3.Cycle)
}

func TestRoundsRerollRefusals(t *testing.T) {
	e := newRounds(t)
	ctx := context.Background()
	sam := e.player(t, e.club, "sam")

	r, err := e.rounds.Create(ctx, e.club.ID, "2026-01", first)
	require.NoError(t, err)
	_, err = e.rounds.Reroll(ctx, e.club.ID, r.ID, first)
	require.ErrorIs(t, err, ErrNoOtherPicker, "sole player")
	_, err = e.rounds.Reroll(ctx, e.other.ID, r.ID, first)
	require.ErrorIs(t, err, ErrNotFound, "other club")

	require.NoError(t, e.rounds.SetTheme(ctx, e.club.ID, r.ID, sam.ID, "Rain", time.Now()))
	_, err = e.rounds.Reroll(ctx, e.club.ID, r.ID, first)
	require.ErrorIs(t, err, ErrWrongPhase, "after theme")
}

func TestRoundsSetTheme(t *testing.T) {
	e := newRounds(t)
	ctx := context.Background()
	sam, kit := e.player(t, e.club, "sam"), e.player(t, e.club, "kit")
	_, err := e.rounds.db.Exec(`UPDATE clubs SET submission_days = 10 WHERE id = ?`, e.club.ID)
	require.NoError(t, err)

	r, err := e.rounds.Create(ctx, e.club.ID, "2026-01", first)
	require.NoError(t, err)
	require.Equal(t, sam.ID, r.PickerID)
	now := time.Date(2026, 1, 5, 12, 30, 0, 0, time.UTC)

	require.ErrorIs(t, e.rounds.SetTheme(ctx, e.club.ID, r.ID, kit.ID, "Rain", now), ErrNotPicker, "non-picker")
	require.ErrorIs(t, e.rounds.SetTheme(ctx, e.other.ID, r.ID, sam.ID, "Rain", now), ErrNotFound, "other club")
	got, _ := e.rounds.Get(ctx, e.club.ID, r.ID)
	require.Equal(t, domain.PhaseThemePending, got.Phase, "rejected writes changed the round")

	require.NoError(t, e.rounds.SetTheme(ctx, e.club.ID, r.ID, sam.ID, "Rain", now))
	got, err = e.rounds.ByMonth(ctx, e.club.ID, "2026-01")
	require.NoError(t, err)
	require.Equal(t, domain.PhaseSubmitting, got.Phase)
	require.Equal(t, "Rain", got.Theme)
	wantDue := time.Date(2026, 1, 15, 12, 30, 0, 0, time.UTC)
	require.NotNil(t, got.SubmissionsDueAt)
	require.True(t, got.SubmissionsDueAt.Equal(wantDue), "due = %v, want %v", got.SubmissionsDueAt, wantDue)

	require.ErrorIs(t, e.rounds.SetTheme(ctx, e.club.ID, r.ID, sam.ID, "Sun", now), ErrWrongPhase, "second SetTheme")
}

func TestRoundsAreClubScoped(t *testing.T) {
	e := newRounds(t)
	ctx := context.Background()
	e.player(t, e.club, "sam")
	e.player(t, e.other, "bob")

	r, err := e.rounds.Create(ctx, e.club.ID, "2026-01", first)
	require.NoError(t, err)
	_, err = e.rounds.Get(ctx, e.other.ID, r.ID)
	require.ErrorIs(t, err, ErrNotFound, "Get across clubs")
	_, err = e.rounds.ByMonth(ctx, e.other.ID, "2026-01")
	require.ErrorIs(t, err, ErrNotFound, "ByMonth across clubs")
	_, err = e.rounds.Current(ctx, e.other.ID)
	require.ErrorIs(t, err, ErrNotFound, "Current across clubs")
	// The other club is free to run the same month at the same time.
	_, err = e.rounds.Create(ctx, e.other.ID, "2026-01", first)
	require.NoError(t, err, "other club's round")
}
