package store

import (
	"context"
	"errors"
	"strings"
	"testing"

	"musiclub/internal/db"
	"musiclub/internal/domain"
)

func newPlayers(t *testing.T) (*Players, domain.Club, domain.Club) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	clubs := NewClubs(d)
	a, err := clubs.Create(context.Background(), "a", "A")
	if err != nil {
		t.Fatal(err)
	}
	b, err := clubs.Create(context.Background(), "b", "B")
	if err != nil {
		t.Fatal(err)
	}
	return NewPlayers(d), a, b
}

func TestPlayersCreateAndGet(t *testing.T) {
	s, club, _ := newPlayers(t)
	ctx := context.Background()

	p, err := s.Create(ctx, club.ID, "  SAM ", true)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "sam" || !p.Active || !p.IsAdmin || p.ClubID != club.ID {
		t.Fatalf("unexpected player: %+v", p)
	}

	q, err := s.Create(ctx, club.ID, "Kit", false)
	if err != nil || q.IsAdmin {
		t.Fatalf("Create non-admin = %+v, %v", q, err)
	}
	got, err := s.List(ctx, club.ID)
	if err != nil || len(got) != 2 || got[0].Name != "kit" {
		t.Fatalf("List = %+v, %v", got, err)
	}
}

func TestPlayersCreateValidation(t *testing.T) {
	s, club, other := newPlayers(t)
	ctx := context.Background()

	if _, err := s.Create(ctx, club.ID, "Sam", false); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		clubID int64
		name   string
		want   error
	}{
		{club.ID, "sam", ErrPlayerNameTaken},
		{club.ID, "SAM", ErrPlayerNameTaken}, // case-insensitive
		{club.ID, "   ", ErrPlayerNameRequired},
		{club.ID, strings.Repeat("x", 51), ErrPlayerNameTooLong},
	}
	for _, c := range cases {
		if _, err := s.Create(ctx, c.clubID, c.name, false); !errors.Is(err, c.want) {
			t.Errorf("Create(%q) err = %v, want %v", c.name, err, c.want)
		}
	}
	// The same name is fine in a different club.
	if _, err := s.Create(ctx, other.ID, "Sam", false); err != nil {
		t.Fatalf("same name in other club: %v", err)
	}
}

func TestPlayersLastAdminGuard(t *testing.T) {
	s, club, _ := newPlayers(t)
	ctx := context.Background()

	admin, _ := s.Create(ctx, club.ID, "Admin", true)
	if err := s.SetAdmin(ctx, club.ID, admin.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("revoke last admin err = %v, want ErrLastAdmin", err)
	}
	if err := s.SetActive(ctx, club.ID, admin.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("retire last admin err = %v, want ErrLastAdmin", err)
	}

	second, _ := s.Create(ctx, club.ID, "Second", true)
	if err := s.SetActive(ctx, club.ID, admin.ID, false); err != nil {
		t.Fatalf("retire with another admin: %v", err)
	}
	got, _ := s.Get(ctx, club.ID, admin.ID)
	if got.Active {
		t.Fatal("player should be inactive")
	}
	// The remaining admin is now the last one.
	if err := s.SetAdmin(ctx, club.ID, second.ID, false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("err = %v, want ErrLastAdmin", err)
	}
	// An inactive admin doesn't count, and reinstating is always allowed.
	if err := s.SetActive(ctx, club.ID, admin.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAdmin(ctx, club.ID, second.ID, false); err != nil {
		t.Fatalf("revoke with another active admin: %v", err)
	}
}

// Two-club test (DESIGN.md §14): nothing leaks between clubs.
func TestPlayersAreClubScoped(t *testing.T) {
	s, a, b := newPlayers(t)
	ctx := context.Background()

	pa, _ := s.Create(ctx, a.ID, "Alice", true)
	pb, _ := s.Create(ctx, b.ID, "Bob", true)

	if got, _ := s.List(ctx, a.ID); len(got) != 1 || got[0].ID != pa.ID {
		t.Fatalf("club a list = %+v", got)
	}
	if _, err := s.Get(ctx, a.ID, pb.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get other club's player err = %v, want ErrNotFound", err)
	}
	if err := s.SetActive(ctx, a.ID, pb.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetActive other club's player err = %v, want ErrNotFound", err)
	}
	if err := s.SetAdmin(ctx, a.ID, pb.ID, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetAdmin other club's player err = %v, want ErrNotFound", err)
	}
	if got, _ := s.Get(ctx, b.ID, pb.ID); !got.Active || !got.IsAdmin {
		t.Fatalf("other club's player was modified: %+v", got)
	}
}
