package scoring

import (
	"sort"
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
	Kind         string // favorite_vote, best_fit_vote, favorite_winner, best_fit_winner, trendsetter
	Points       int
	SubmissionID *int64
	BallotID     *int64
}

type Rules struct {
	VotePoints        int // 1
	CategoryWinBonus  int // 1 (per category won)
	TrendsetterPerDup int // 1
}

func DefaultRules() Rules {
	return Rules{
		VotePoints:        1,
		CategoryWinBonus:  1,
		TrendsetterPerDup: 1,
	}
}

type Tallies struct {
	FavoriteVotes map[int64]int
	BestFitVotes  map[int64]int
	OverallVotes  map[int64]int
	FavoriteMax   int
	BestFitMax    int
	OverallMax    int
	FavoriteWins  []int64 // submission IDs (deterministic order)
	BestFitWins   []int64 // submission IDs (deterministic order)
	OverallWins   []int64 // submission IDs (deterministic order)
}

// Tally counts votes and finds category winners. It is reused by scoring and stats.
func Tally(r Results) Tallies {
	t := Tallies{
		FavoriteVotes: make(map[int64]int),
		BestFitVotes:  make(map[int64]int),
		OverallVotes:  make(map[int64]int),
	}

	for _, s := range r.Submissions {
		t.FavoriteVotes[s.ID] = 0
		t.BestFitVotes[s.ID] = 0
		t.OverallVotes[s.ID] = 0
	}

	for _, b := range r.Ballots {
		t.FavoriteVotes[b.FavoriteSubmissionID]++
		t.BestFitVotes[b.BestFitSubmissionID]++
		t.OverallVotes[b.FavoriteSubmissionID]++
		t.OverallVotes[b.BestFitSubmissionID]++
	}

	for _, count := range t.FavoriteVotes {
		if count > t.FavoriteMax {
			t.FavoriteMax = count
		}
	}
	for _, count := range t.BestFitVotes {
		if count > t.BestFitMax {
			t.BestFitMax = count
		}
	}
	for _, count := range t.OverallVotes {
		if count > t.OverallMax {
			t.OverallMax = count
		}
	}

	for _, s := range r.Submissions {
		if t.FavoriteMax > 0 && t.FavoriteVotes[s.ID] == t.FavoriteMax {
			t.FavoriteWins = append(t.FavoriteWins, s.ID)
		}
		if t.BestFitMax > 0 && t.BestFitVotes[s.ID] == t.BestFitMax {
			t.BestFitWins = append(t.BestFitWins, s.ID)
		}
		if t.OverallMax > 0 && t.OverallVotes[s.ID] == t.OverallMax {
			t.OverallWins = append(t.OverallWins, s.ID)
		}
	}

	return t
}

// Score computes all score events for a round's results according to rules.
func Score(r Results, rules Rules) []Event {
	var events []Event

	subByID := make(map[int64]Submission, len(r.Submissions))
	for _, s := range r.Submissions {
		subByID[s.ID] = s
	}

	// Deterministic ballot ordering
	sortedBallots := make([]Ballot, len(r.Ballots))
	copy(sortedBallots, r.Ballots)
	sort.Slice(sortedBallots, func(i, j int) bool { return sortedBallots[i].ID < sortedBallots[j].ID })

	// 1. Vote points
	if rules.VotePoints > 0 {
		for _, b := range sortedBallots {
			if s, ok := subByID[b.FavoriteSubmissionID]; ok {
				subID := s.ID
				bID := b.ID
				events = append(events, Event{
					PlayerID:     s.PlayerID,
					Kind:         "favorite_vote",
					Points:       rules.VotePoints,
					SubmissionID: &subID,
					BallotID:     &bID,
				})
			}
			if s, ok := subByID[b.BestFitSubmissionID]; ok {
				subID := s.ID
				bID := b.ID
				events = append(events, Event{
					PlayerID:     s.PlayerID,
					Kind:         "best_fit_vote",
					Points:       rules.VotePoints,
					SubmissionID: &subID,
					BallotID:     &bID,
				})
			}
		}
	}

	// 2. Category winners
	tallies := Tally(r)
	if rules.CategoryWinBonus > 0 {
		for _, subID := range tallies.FavoriteWins {
			s := subByID[subID]
			id := s.ID
			events = append(events, Event{
				PlayerID:     s.PlayerID,
				Kind:         "favorite_winner",
				Points:       rules.CategoryWinBonus,
				SubmissionID: &id,
			})
		}
		for _, subID := range tallies.BestFitWins {
			s := subByID[subID]
			id := s.ID
			events = append(events, Event{
				PlayerID:     s.PlayerID,
				Kind:         "best_fit_winner",
				Points:       rules.CategoryWinBonus,
				SubmissionID: &id,
			})
		}
	}

	// 3. Trendsetter points
	if rules.TrendsetterPerDup > 0 {
		events = append(events, trendsetter(r.Submissions, rules.TrendsetterPerDup)...)
	}

	return events
}

func trendsetter(submissions []Submission, bonus int) []Event {
	var events []Event

	byKey := make(map[string][]Submission)
	for _, s := range submissions {
		byKey[s.SongKey] = append(byKey[s.SongKey], s)
	}

	// Deterministic key order
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		group := byKey[k]
		if len(group) <= 1 {
			continue
		}

		// Earliest submitted (ties fall back to lowest ID)
		earliest := group[0]
		for _, s := range group[1:] {
			if s.SubmittedAt.Before(earliest.SubmittedAt) || (s.SubmittedAt.Equal(earliest.SubmittedAt) && s.ID < earliest.ID) {
				earliest = s
			}
		}

		// Sort others by ID for determinism
		sort.Slice(group, func(i, j int) bool { return group[i].ID < group[j].ID })

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
