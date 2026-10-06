package store

import (
	"context"
	"errors"
	"testing"

	"musiclub/internal/db"
)

func newClubs(t *testing.T) *Clubs {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return NewClubs(d)
}

func TestClubsCreateAndLookup(t *testing.T) {
	s, ctx := newClubs(t), context.Background()

	c, err := s.Create(ctx, "friday-friends", "  Friday Friends ")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "Friday Friends" || c.SubmissionDays != 14 || c.VotingDays != 14 {
		t.Fatalf("unexpected club: %+v", c)
	}

	got, err := s.BySlug(ctx, "friday-friends")
	if err != nil || got.ID != c.ID {
		t.Fatalf("BySlug = %+v, %v", got, err)
	}
	if _, err := s.BySlug(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("BySlug missing: err = %v, want ErrNotFound", err)
	}
}

func TestClubsCreateValidation(t *testing.T) {
	s, ctx := newClubs(t), context.Background()

	if _, err := s.Create(ctx, "one", "One"); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		slug, name string
		want       error
	}{
		{"one", "Again", ErrSlugTaken},
		{"Bad Slug", "x", ErrInvalidSlug},
		{"", "x", ErrInvalidSlug},
		{"-lead", "x", ErrInvalidSlug},
		{"fine", "  ", ErrNameRequired},
	}
	for _, c := range cases {
		if _, err := s.Create(ctx, c.slug, c.name); !errors.Is(err, c.want) {
			t.Errorf("Create(%q, %q) err = %v, want %v", c.slug, c.name, err, c.want)
		}
	}
}

func TestClubsList(t *testing.T) {
	s, ctx := newClubs(t), context.Background()
	for slug, name := range map[string]string{"b": "Beta", "a": "Alpha"} {
		if _, err := s.Create(ctx, slug, name); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.List(ctx)
	if err != nil || len(got) != 2 || got[0].Slug != "a" {
		t.Fatalf("List = %+v, %v", got, err)
	}
}
