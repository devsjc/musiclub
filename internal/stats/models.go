package stats

import "musiclub/internal/domain"

type SongReveal struct {
	Submission    domain.Submission
	SubmitterName string
}

type RoundSummary struct {
	RoundID        int64
	Winners        Winners
	Songs          []SongReveal
	PlayerPoints   []PlayerPoints
	GuessStats     GuessStats
	FeaturedQuotes []QuoteView
}

type QuoteView struct {
	Quote         domain.FeaturedQuote
	SubmitterName string
	VoterName     string
}

type Winners struct {
	Favorite []WinnerView
	BestFit  []WinnerView
	Overall  []WinnerView
}

type WinnerView struct {
	Submission    domain.Submission
	SubmitterName string
	Votes         int
}

type PlayerPoints struct {
	PlayerID      int64
	Name          string
	Total         int
	FavoriteVotes int
	BestFitVotes  int
	FavoriteWins  int
	BestFitWins   int
	Trendsetter   int
}

type GuessStats struct {
	OverallCorrectPct float64
	ByGuesser         []GuesserStat
	BySubmitter       []SubmitterStat // Guessability
}

type GuesserStat struct {
	PlayerID int64
	Name     string
	Correct  int
	Total    int
	Pct      float64
}

type SubmitterStat struct {
	PlayerID int64
	Name     string
	Correct  int
	Total    int
	Pct      float64
}

type PlayerMatrixHeader struct {
	ID   int64
	Name string
}

type ClubStats struct {
	ClubID        int64
	ClosedRounds  int // published rounds; 0 means there is nothing to show yet
	Leaderboard   []LeaderboardRow
	Honours       []HonourRow
	GuessStats    GuessStats
	GuessMatrix   map[int64]map[int64]float64 // guesserID -> submitterID -> correctPct
	MatrixPlayers []PlayerMatrixHeader        // sorted deterministically for matrix rendering
}

type LeaderboardRow struct {
	PlayerID     int64
	Name         string
	TotalPoints  int
	RoundsPlayed int
	AvgPerRound  float64
}

type HonourRow struct {
	PlayerID     int64
	Name         string
	FavoriteWins int
	BestFitWins  int
	OverallWins  int
	Trendsetter  int
}

type PlayerProfile struct {
	Player       domain.Player
	TotalPoints  int
	RoundsPlayed int
	Honours      HonourRow
	Songs        []PlayerSongReveal
}

type PlayerSongReveal struct {
	Month  string
	Title  string
	Artist string
	URL    string
}
