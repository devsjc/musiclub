package rounds

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"musiclub/internal/domain"
	"musiclub/internal/scoring"
	"musiclub/internal/store"
)

var ErrQuotesNotCurated = errors.New("no featured quotes selected for winning songs; check confirm to publish anyway")

type CandidateComment struct {
	BallotID     int64
	SubmissionID int64
	Category     domain.QuoteCategory
	SongTitle    string
	Comment      string
}

type ReviewData struct {
	Round           domain.Round
	Submissions     []domain.Submission
	Tallies         scoring.Tallies
	WinningSubs     map[int64]domain.Submission
	CandidateQuotes []CandidateComment
	FeaturedQuotes  []domain.FeaturedQuote
}

func (s *Service) Review(ctx context.Context, clubID, roundID int64) (ReviewData, error) {
	r, err := s.rounds.Get(ctx, clubID, roundID)
	if err != nil {
		return ReviewData{}, err
	}
	if r.Phase != domain.PhaseReview && r.Phase != domain.PhaseClosed {
		return ReviewData{}, store.ErrWrongPhase
	}

	subs, err := s.subs.List(ctx, clubID, roundID)
	if err != nil {
		return ReviewData{}, err
	}
	subMap := make(map[int64]domain.Submission, len(subs))
	for _, sub := range subs {
		subMap[sub.ID] = sub
	}

	ballots, err := s.ballots.List(ctx, clubID, roundID)
	if err != nil {
		return ReviewData{}, err
	}

	res := s.toScoringResults(subs, ballots)
	tallies := scoring.Tally(res)

	// Collect winning submissions
	winningSubs := make(map[int64]domain.Submission)
	for _, id := range append(tallies.FavoriteWins, tallies.BestFitWins...) {
		if sub, ok := subMap[id]; ok {
			winningSubs[id] = sub
		}
	}

	// Collect candidate quotes from voters' comments on the winning songs
	var candidates []CandidateComment
	favWinMap := make(map[int64]bool, len(tallies.FavoriteWins))
	for _, id := range tallies.FavoriteWins {
		favWinMap[id] = true
	}
	bestWinMap := make(map[int64]bool, len(tallies.BestFitWins))
	for _, id := range tallies.BestFitWins {
		bestWinMap[id] = true
	}

	for _, b := range ballots {
		if favWinMap[b.FavoriteSubmissionID] && strings.TrimSpace(b.FavoriteComment) != "" {
			candidates = append(candidates, CandidateComment{
				BallotID:     b.ID,
				SubmissionID: b.FavoriteSubmissionID,
				Category:     domain.QuoteCategoryFavorite,
				SongTitle:    subMap[b.FavoriteSubmissionID].Artist + " — " + subMap[b.FavoriteSubmissionID].Title,
				Comment:      b.FavoriteComment,
			})
		}
		if bestWinMap[b.BestFitSubmissionID] && strings.TrimSpace(b.BestFitComment) != "" {
			candidates = append(candidates, CandidateComment{
				BallotID:     b.ID,
				SubmissionID: b.BestFitSubmissionID,
				Category:     domain.QuoteCategoryBestFit,
				SongTitle:    subMap[b.BestFitSubmissionID].Artist + " — " + subMap[b.BestFitSubmissionID].Title,
				Comment:      b.BestFitComment,
			})
		}
	}

	featured, err := s.quotes.List(ctx, clubID, roundID)
	if err != nil {
		return ReviewData{}, err
	}

	return ReviewData{
		Round:           r,
		Submissions:     subs,
		Tallies:         tallies,
		WinningSubs:     winningSubs,
		CandidateQuotes: candidates,
		FeaturedQuotes:  featured,
	}, nil
}

func (s *Service) SetSongKey(ctx context.Context, clubID, roundID, submissionID int64, key string) error {
	r, err := s.rounds.Get(ctx, clubID, roundID)
	if err != nil {
		return err
	}
	if r.Phase != domain.PhaseReview && r.Phase != domain.PhaseClosed {
		return store.ErrWrongPhase
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errors.New("song key cannot be empty")
	}
	return s.subs.SetSongKey(ctx, clubID, roundID, submissionID, key)
}

func (s *Service) AddFeaturedQuote(ctx context.Context, clubID, roundID, submissionID int64, category domain.QuoteCategory, ballotID *int64, quote string) error {
	r, err := s.rounds.Get(ctx, clubID, roundID)
	if err != nil {
		return err
	}
	if r.Phase != domain.PhaseReview {
		return store.ErrWrongPhase
	}
	quote = strings.TrimSpace(quote)
	if quote == "" {
		return errors.New("quote cannot be empty")
	}

	// Validate that the submission won in this category (DESIGN.md §6.1)
	res, err := s.getScoringResults(ctx, clubID, roundID)
	if err != nil {
		return err
	}
	tallies := scoring.Tally(res)
	var won bool
	if category == domain.QuoteCategoryFavorite {
		for _, id := range tallies.FavoriteWins {
			if id == submissionID {
				won = true
				break
			}
		}
	} else if category == domain.QuoteCategoryBestFit {
		for _, id := range tallies.BestFitWins {
			if id == submissionID {
				won = true
				break
			}
		}
	}
	if !won {
		return fmt.Errorf("quotes can only be attached to a winning submission in its winning category")
	}

	return s.quotes.Add(ctx, clubID, roundID, domain.FeaturedQuote{
		RoundID:      roundID,
		Category:     category,
		SubmissionID: submissionID,
		BallotID:     ballotID,
		Quote:        quote,
	})
}

func (s *Service) DeleteFeaturedQuote(ctx context.Context, clubID, roundID, quoteID int64) error {
	r, err := s.rounds.Get(ctx, clubID, roundID)
	if err != nil {
		return err
	}
	if r.Phase != domain.PhaseReview {
		return store.ErrWrongPhase
	}
	return s.quotes.Delete(ctx, clubID, roundID, quoteID)
}

func (s *Service) Publish(ctx context.Context, clubID, roundID int64, force bool) error {
	r, err := s.rounds.Get(ctx, clubID, roundID)
	if err != nil {
		return err
	}
	if r.Phase != domain.PhaseReview {
		return store.ErrWrongPhase
	}

	res, err := s.getScoringResults(ctx, clubID, roundID)
	if err != nil {
		return err
	}

	// Unless force is true, refuse if there are winning categories without quotes (DESIGN.md §10)
	if !force {
		tallies := scoring.Tally(res)
		quotes, err := s.quotes.List(ctx, clubID, roundID)
		if err != nil {
			return err
		}
		if (len(tallies.FavoriteWins) > 0 || len(tallies.BestFitWins) > 0) && len(quotes) == 0 {
			return ErrQuotesNotCurated
		}
	}

	events := scoring.Score(res, scoring.DefaultRules())
	var domainEvents []domain.ScoreEvent
	for _, e := range events {
		domainEvents = append(domainEvents, domain.ScoreEvent{
			RoundID:      roundID,
			PlayerID:     e.PlayerID,
			Kind:         domain.ScoreKind(e.Kind),
			Points:       e.Points,
			SubmissionID: e.SubmissionID,
			BallotID:     e.BallotID,
		})
	}

	return s.rounds.Publish(ctx, clubID, roundID, domainEvents, s.now())
}

func (s *Service) Recompute(ctx context.Context, clubID, roundID int64) error {
	res, err := s.getScoringResults(ctx, clubID, roundID)
	if err != nil {
		return err
	}

	events := scoring.Score(res, scoring.DefaultRules())
	var domainEvents []domain.ScoreEvent
	for _, e := range events {
		domainEvents = append(domainEvents, domain.ScoreEvent{
			RoundID:      roundID,
			PlayerID:     e.PlayerID,
			Kind:         domain.ScoreKind(e.Kind),
			Points:       e.Points,
			SubmissionID: e.SubmissionID,
			BallotID:     e.BallotID,
		})
	}

	return s.rounds.Recompute(ctx, clubID, roundID, domainEvents)
}

func (s *Service) getScoringResults(ctx context.Context, clubID, roundID int64) (scoring.Results, error) {
	subs, err := s.subs.List(ctx, clubID, roundID)
	if err != nil {
		return scoring.Results{}, err
	}
	ballots, err := s.ballots.List(ctx, clubID, roundID)
	if err != nil {
		return scoring.Results{}, err
	}
	return s.toScoringResults(subs, ballots), nil
}

func (s *Service) toScoringResults(subs []domain.Submission, ballots []domain.Ballot) scoring.Results {
	var res scoring.Results
	for _, sub := range subs {
		res.Submissions = append(res.Submissions, scoring.Submission{
			ID:          sub.ID,
			PlayerID:    sub.PlayerID,
			SongKey:     sub.SongKey,
			SubmittedAt: sub.SubmittedAt,
		})
	}
	for _, b := range ballots {
		res.Ballots = append(res.Ballots, scoring.Ballot{
			ID:                   b.ID,
			VoterID:              b.VoterID,
			FavoriteSubmissionID: b.FavoriteSubmissionID,
			BestFitSubmissionID:  b.BestFitSubmissionID,
		})
	}
	return res
}
