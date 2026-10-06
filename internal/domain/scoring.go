package domain

type ScoreKind string

const (
	ScoreFavoriteVote   ScoreKind = "favorite_vote"
	ScoreBestFitVote    ScoreKind = "best_fit_vote"
	ScoreFavoriteWinner ScoreKind = "favorite_winner"
	ScoreBestFitWinner  ScoreKind = "best_fit_winner"
	ScoreTrendsetter    ScoreKind = "trendsetter"
)

type ScoreEvent struct {
	ID           int64
	RoundID      int64
	PlayerID     int64
	Kind         ScoreKind
	Points       int
	SubmissionID *int64
	BallotID     *int64
}
