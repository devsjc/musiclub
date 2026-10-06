package rounds

import (
	"context"

	"musiclub/internal/domain"
	"musiclub/internal/scoring"
	"musiclub/internal/store"
)

func (s *Service) SetSongKey(ctx context.Context, clubID, roundID, submissionID int64, key string) error {
	return s.subs.SetSongKey(ctx, clubID, roundID, submissionID, key)
}

func (s *Service) SetQuotes(ctx context.Context, clubID, roundID int64, quotes []domain.FeaturedQuote) error {
	// You might want to validate that the quotes refer to winning submissions and categories
	return s.quotes.Set(ctx, clubID, roundID, quotes)
}

func (s *Service) Publish(ctx context.Context, clubID, roundID int64) error {
	// Check phase and get submissions/ballots
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

	rules := scoring.Rules{
		VotePoints:        1,
		CategoryWinBonus:  1,
		TrendsetterPerDup: 1,
	}

	events := scoring.Score(res, rules)

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

	rules := scoring.Rules{
		VotePoints:        1,
		CategoryWinBonus:  1,
		TrendsetterPerDup: 1,
	}

	events := scoring.Score(res, rules)

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
	var res scoring.Results

	subs, err := s.subs.List(ctx, clubID, roundID)
	if err != nil {
		return res, err
	}
	for _, sub := range subs {
		res.Submissions = append(res.Submissions, scoring.Submission{
			ID:          sub.ID,
			PlayerID:    sub.PlayerID,
			SongKey:     sub.SongKey,
			SubmittedAt: sub.SubmittedAt,
		})
	}

	ballots, err := s.ballots.List(ctx, clubID, roundID)
	if err != nil {
		return res, err
	}
	for _, b := range ballots {
		res.Ballots = append(res.Ballots, scoring.Ballot{
			ID:                   b.ID,
			VoterID:              b.VoterID,
			FavoriteSubmissionID: b.FavoriteSubmissionID,
			BestFitSubmissionID:  b.BestFitSubmissionID,
		})
	}

	return res, nil
}
