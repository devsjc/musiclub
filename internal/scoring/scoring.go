package scoring

import (
	"time"
)

type Submission struct {
	ID          int64
	PlayerID    int64
	SongKey     string
	SubmittedAt time.Time
}

type Ballot struct {
	ID                   int64
	VoterID              int64
	FavoriteSubmissionID int64
	BestFitSubmissionID  int64
}

type Results struct {
	Submissions []Submission
	Ballots     []Ballot
}

type Event struct {
	PlayerID     int64
	Kind         string // domain.ScoreFavoriteVote, etc.
	Points       int
	SubmissionID *int64
	BallotID     *int64
}

type Rules struct {
	VotePoints        int // 1
	CategoryWinBonus  int // 1 (per category won)
	TrendsetterPerDup int // 1
}

// Score is the single entry point.
func Score(r Results, rules Rules) []Event {
	var events []Event

	// We need quick lookups for submissions by ID
	subByID := make(map[int64]Submission)
	for _, s := range r.Submissions {
		subByID[s.ID] = s
	}

	// 1. Vote Points
	favCounts := make(map[int64]int)
	bestFitCounts := make(map[int64]int)

	for _, b := range r.Ballots {
		if s, ok := subByID[b.FavoriteSubmissionID]; ok {
			favCounts[s.ID]++
			if rules.VotePoints > 0 {
				subID := s.ID
				ballotID := b.ID
				events = append(events, Event{
					PlayerID:     s.PlayerID,
					Kind:         "favorite_vote",
					Points:       rules.VotePoints,
					SubmissionID: &subID,
					BallotID:     &ballotID,
				})
			}
		}

		if s, ok := subByID[b.BestFitSubmissionID]; ok {
			bestFitCounts[s.ID]++
			if rules.VotePoints > 0 {
				subID := s.ID
				ballotID := b.ID
				events = append(events, Event{
					PlayerID:     s.PlayerID,
					Kind:         "best_fit_vote",
					Points:       rules.VotePoints,
					SubmissionID: &subID,
					BallotID:     &ballotID,
				})
			}
		}
	}

	// 2. Category Winners
	if rules.CategoryWinBonus > 0 {
		events = append(events, categoryWinners(favCounts, subByID, "favorite_winner", rules.CategoryWinBonus)...)
		events = append(events, categoryWinners(bestFitCounts, subByID, "best_fit_winner", rules.CategoryWinBonus)...)
	}

	// 3. Trendsetter
	if rules.TrendsetterPerDup > 0 {
		events = append(events, trendsetter(r.Submissions, rules.TrendsetterPerDup)...)
	}

	return events
}

func categoryWinners(counts map[int64]int, subByID map[int64]Submission, kind string, bonus int) []Event {
	var max int
	for _, count := range counts {
		if count > max {
			max = count
		}
	}

	var events []Event
	if max == 0 {
		return events
	}

	for id, count := range counts {
		if count == max {
			if s, ok := subByID[id]; ok {
				subID := s.ID
				events = append(events, Event{
					PlayerID:     s.PlayerID,
					Kind:         kind,
					Points:       bonus,
					SubmissionID: &subID,
				})
			}
		}
	}
	return events
}

func trendsetter(submissions []Submission, bonus int) []Event {
	var events []Event
	
	byKey := make(map[string][]Submission)
	for _, s := range submissions {
		byKey[s.SongKey] = append(byKey[s.SongKey], s)
	}

	for _, group := range byKey {
		if len(group) <= 1 {
			continue
		}

		// find earliest
		earliest := group[0]
		for _, s := range group[1:] {
			if s.SubmittedAt.Before(earliest.SubmittedAt) || (s.SubmittedAt.Equal(earliest.SubmittedAt) && s.ID < earliest.ID) {
				earliest = s
			}
		}

		// Give points per other submission
		for _, s := range group {
			if s.ID == earliest.ID {
				continue
			}
			subID := s.ID
			events = append(events, Event{
				PlayerID:     earliest.PlayerID,
				Kind:         "trendsetter",
				Points:       bonus,
				SubmissionID: &subID,
			})
		}
	}

	return events
}
