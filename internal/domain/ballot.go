package domain

import "time"

type Guess struct {
	SubmissionID    int64
	GuessedPlayerID int64
}

type Ballot struct {
	ID                   int64
	RoundID              int64
	VoterID              int64
	FavoriteSubmissionID int64
	FavoriteComment      string
	BestFitSubmissionID  int64
	BestFitComment       string
	SubmittedAt          time.Time
	Guesses              []Guess
}
