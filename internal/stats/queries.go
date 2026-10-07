package stats

import (
	"context"
)

// MonthSummary computes stats for a single closed round.
func (s *Store) MonthSummary(ctx context.Context, roundID int64) (RoundSummary, error) {
	var summary RoundSummary
	summary.RoundID = roundID

	// Points
	points, err := s.roundPoints(ctx, roundID)
	if err != nil {
		return summary, err
	}
	summary.PlayerPoints = points

	// Guesses
	guesses, err := s.roundGuessStats(ctx, roundID)
	if err != nil {
		return summary, err
	}
	summary.GuessStats = guesses

	// Quotes
	quotes, err := s.roundQuotes(ctx, roundID)
	if err != nil {
		return summary, err
	}
	summary.FeaturedQuotes = quotes

	// Winners and revealed songs
	winners, songs, err := s.roundWinnersAndSongs(ctx, roundID)
	if err != nil {
		return summary, err
	}
	summary.Winners = winners
	summary.Songs = songs

	return summary, nil
}
