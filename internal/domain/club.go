// Package domain holds plain structs and enums shared across layers.
package domain

import "time"

type Club struct {
	ID             int64
	Slug           string
	Name           string
	SubmissionDays int
	VotingDays     int
	CreatedAt      time.Time
}
