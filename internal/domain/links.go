package domain

import "time"

type SubmissionLink struct {
	SubmissionID int64
	Service      string
	URL          string
	ExternalID   string
	CreatedAt    time.Time
}
