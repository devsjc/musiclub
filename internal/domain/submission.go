package domain

import "time"

// Submission is a player's song for a round.
type Submission struct {
	ID          int64
	RoundID     int64
	PlayerID    int64
	Title       string
	Artist      string
	URL         string // primary listening link
	URLService  string // youtube_music | spotify | qobuz | other
	LinkSource  string // matched | manual
	ExternalID  string // the service's ID when matched; empty otherwise
	Note        string
	SongKey     string // normalised duplicate key (DESIGN.md §5.3)
	SubmittedAt time.Time
	Position    *int // shuffled display order, set when voting opens
}
