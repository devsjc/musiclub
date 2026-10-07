package domain

import "time"

// Phase is a step in a round's lifecycle (DESIGN.md §4).
type Phase string

const (
	PhaseThemePending Phase = "theme_pending"
	PhaseSubmitting   Phase = "submitting"
	PhaseVoting       Phase = "voting"
	PhaseReview       Phase = "review"
	PhaseClosed       Phase = "closed"
)

type Round struct {
	ID               int64
	ClubID           int64
	Month            string // "YYYY-MM"
	Cycle            int    // picker rotation cycle within the club
	PickerID         int64
	Theme            string // empty until the picker sets it
	Phase            Phase
	SubmissionsDueAt *time.Time // set when the theme is chosen
	VotingDueAt      *time.Time // set when voting opens
	CreatedAt        time.Time
	ClosedAt         *time.Time
}
