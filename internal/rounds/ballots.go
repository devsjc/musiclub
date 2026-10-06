package rounds

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"musiclub/internal/domain"
)

var ErrInvalidBallot = errors.New("invalid ballot")

// SubmitBallot validates and saves a voter's ballot and guesses for a round.
func (s *Service) SubmitBallot(ctx context.Context, clubID, roundID, voterID int64, form url.Values) error {
	favID, err := strconv.ParseInt(form.Get("favorite_submission_id"), 10, 64)
	if err != nil {
		return fmt.Errorf("%w: missing favorite", ErrInvalidBallot)
	}

	fitID, err := strconv.ParseInt(form.Get("best_fit_submission_id"), 10, 64)
	if err != nil {
		return fmt.Errorf("%w: missing best fit", ErrInvalidBallot)
	}

	if favID == fitID {
		return fmt.Errorf("%w: favorite and best fit must be different songs", ErrInvalidBallot)
	}

	subs, err := s.subs.List(ctx, clubID, roundID)
	if err != nil {
		return err
	}

	subMap := make(map[int64]domain.Submission, len(subs))
	submitterIDs := make(map[int64]bool, len(subs))
	var ownSubID int64
	for _, sub := range subs {
		subMap[sub.ID] = sub
		submitterIDs[sub.PlayerID] = true
		if sub.PlayerID == voterID {
			ownSubID = sub.ID
		}
	}

	if _, ok := subMap[favID]; !ok {
		return fmt.Errorf("%w: invalid favorite song", ErrInvalidBallot)
	}
	if _, ok := subMap[fitID]; !ok {
		return fmt.Errorf("%w: invalid best fit song", ErrInvalidBallot)
	}
	if ownSubID != 0 && (favID == ownSubID || fitID == ownSubID) {
		return fmt.Errorf("%w: cannot vote for your own song", ErrInvalidBallot)
	}

	// Full-assignment check (DESIGN.md §6.1):
	// Guessed players must be exactly the submitters minus the voter.
	eligibleGuessIDs := make(map[int64]bool, len(submitterIDs))
	for pID := range submitterIDs {
		if pID != voterID {
			eligibleGuessIDs[pID] = true
		}
	}

	b := domain.Ballot{
		RoundID:              roundID,
		VoterID:              voterID,
		FavoriteSubmissionID: favID,
		FavoriteComment:      strings.TrimSpace(form.Get("favorite_comment")),
		BestFitSubmissionID:  fitID,
		BestFitComment:       strings.TrimSpace(form.Get("best_fit_comment")),
	}

	usedPlayers := make(map[int64]bool, len(eligibleGuessIDs))
	for _, sub := range subs {
		if sub.ID == ownSubID {
			continue // voter doesn't guess their own song
		}
		guessStr := form.Get(fmt.Sprintf("guess_%d", sub.ID))
		if guessStr == "" {
			return fmt.Errorf("%w: missing guess for a song", ErrInvalidBallot)
		}
		guessedID, err := strconv.ParseInt(guessStr, 10, 64)
		if err != nil || !eligibleGuessIDs[guessedID] {
			return fmt.Errorf("%w: invalid guessed player", ErrInvalidBallot)
		}
		if usedPlayers[guessedID] {
			return fmt.Errorf("%w: cannot guess the same player twice", ErrInvalidBallot)
		}
		usedPlayers[guessedID] = true
		b.Guesses = append(b.Guesses, domain.Guess{
			SubmissionID:    sub.ID,
			GuessedPlayerID: guessedID,
		})
	}

	return s.ballots.Create(ctx, clubID, b, s.now())
}
