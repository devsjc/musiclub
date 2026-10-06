package domain

import "time"

type Player struct {
	ID        int64
	ClubID    int64
	Name      string
	Active    bool // false = retired from future rounds
	IsAdmin   bool
	CreatedAt time.Time
}
