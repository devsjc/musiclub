package stats

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/db"
	"musiclub/internal/domain"
	"musiclub/internal/rounds"
	"musiclub/internal/store"
)

func setupClosedRound(t *testing.T) (*Store, *rounds.Service, domain.Club, domain.Round, domain.Player, domain.Player, domain.Player) {
	t.Helper()
	database, err := db.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	ctx := context.Background()
	clubs := store.NewClubs(database)
	players := store.NewPlayers(database)
	club, err := clubs.Create(ctx, "friends", "Friends Club")
	require.NoError(t, err)

	sam, err := players.Create(ctx, club.ID, "Sam", true)
	require.NoError(t, err)
	kit, err := players.Create(ctx, club.ID, "Kit", false)
	require.NoError(t, err)
	pat, err := players.Create(ctx, club.ID, "Pat", false)
	require.NoError(t, err)

	svc := rounds.New(database, nil)

	// Round
	r, err := svc.StartRound(ctx, club.ID, "2026-03")
	require.NoError(t, err)
	require.NoError(t, svc.SetTheme(ctx, club.ID, r.ID, r.PickerID, "Rain"))

	// Submissions
	sub1, err := svc.SubmitSong(ctx, club.ID, r.ID, sam.ID, rounds.Song{Artist: "Queen", Title: "Song 1", URL: "https://example.com/1"})
	require.NoError(t, err)
	sub2, err := svc.SubmitSong(ctx, club.ID, r.ID, kit.ID, rounds.Song{Artist: "Beatles", Title: "Song 2", URL: "https://example.com/2"})
	require.NoError(t, err)
	sub3, err := svc.SubmitSong(ctx, club.ID, r.ID, pat.ID, rounds.Song{Artist: "Bowie", Title: "Song 3", URL: "https://example.com/3"})
	require.NoError(t, err)

	// Advance to voting
	due := time.Now().AddDate(0, 0, 14).UTC().Format("2006-01-02 15:04:05")
	_, err = database.Exec(`UPDATE rounds SET phase = 'voting', voting_due_at = ? WHERE id = ?`, due, r.ID)
	require.NoError(t, err)

	ballots := store.NewBallots(database)
	// Sam votes: Fav = sub2 (Kit), Fit = sub3 (Pat). Guesses: sub2=Kit, sub3=Pat
	bSam := domain.Ballot{
		RoundID:              r.ID,
		VoterID:              sam.ID,
		FavoriteSubmissionID: sub2.ID,
		FavoriteComment:      "Kit's song is great",
		BestFitSubmissionID:  sub3.ID,
		BestFitComment:       "Pat fits rain",
		Guesses: []domain.Guess{
			{SubmissionID: sub2.ID, GuessedPlayerID: kit.ID},
			{SubmissionID: sub3.ID, GuessedPlayerID: pat.ID},
		},
	}
	require.NoError(t, ballots.Create(ctx, club.ID, bSam, time.Now()))

	// Kit votes: Fav = sub1 (Sam), Fit = sub3 (Pat). Guesses: sub1=Sam, sub3=Pat
	bKit := domain.Ballot{
		RoundID:              r.ID,
		VoterID:              kit.ID,
		FavoriteSubmissionID: sub1.ID,
		FavoriteComment:      "Sam's song is cool",
		BestFitSubmissionID:  sub3.ID,
		BestFitComment:       "Pat's song fits best",
		Guesses: []domain.Guess{
			{SubmissionID: sub1.ID, GuessedPlayerID: sam.ID},
			{SubmissionID: sub3.ID, GuessedPlayerID: pat.ID},
		},
	}
	require.NoError(t, ballots.Create(ctx, club.ID, bKit, time.Now()))

	// Advance to review
	_, err = database.Exec(`UPDATE rounds SET phase = 'review' WHERE id = ?`, r.ID)
	require.NoError(t, err)

	// Feature quote for winning song sub3 (Pat won Best Fit)
	require.NoError(t, svc.AddFeaturedQuote(ctx, club.ID, r.ID, sub3.ID, domain.QuoteCategoryBestFit, nil, "Pat fits rain!"))

	// Publish round (force=true)
	require.NoError(t, svc.Publish(ctx, club.ID, r.ID, true))

	closedRound, err := svc.StartRound(ctx, club.ID, "2026-04") // just to verify, wait, club round:
	_ = closedRound
	r, err = store.NewRounds(database).Get(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PhaseClosed, r.Phase)

	statsStore := NewStore(database)
	return statsStore, svc, club, r, sam, kit, pat
}

func TestMonthSummary(t *testing.T) {
	statsStore, _, _, r, sam, kit, pat := setupClosedRound(t)
	ctx := context.Background()

	summary, err := statsStore.MonthSummary(ctx, r.ID)
	require.NoError(t, err)
	require.Equal(t, r.ID, summary.RoundID)

	// Best Fit Winner should be Pat's song (2 votes)
	require.Len(t, summary.Winners.BestFit, 1)
	require.Equal(t, "Bowie", summary.Winners.BestFit[0].Submission.Artist)
	require.Equal(t, pat.Name, summary.Winners.BestFit[0].SubmitterName)
	require.Equal(t, 2, summary.Winners.BestFit[0].Votes)

	// Overall Winner should be Pat's song (2 votes total)
	require.Len(t, summary.Winners.Overall, 1)
	require.Equal(t, pat.Name, summary.Winners.Overall[0].SubmitterName)

	// All songs reveal
	require.Len(t, summary.Songs, 3)

	// Featured quotes
	require.Len(t, summary.FeaturedQuotes, 1)
	require.Equal(t, "Pat fits rain!", summary.FeaturedQuotes[0].Quote.Quote)

	// Guess stats: 4 guesses cast total, all 4 correct = 100%
	require.Equal(t, 100.0, summary.GuessStats.OverallCorrectPct)

	// Player points breakdown
	require.NotEmpty(t, summary.PlayerPoints)
	_ = sam
	_ = kit
}

func TestClubStats(t *testing.T) {
	statsStore, _, club, _, _, _, _ := setupClosedRound(t)
	ctx := context.Background()

	cs, err := statsStore.ClubStats(ctx, club.ID)
	require.NoError(t, err)

	require.Equal(t, club.ID, cs.ClubID)
	require.NotEmpty(t, cs.Leaderboard)
	require.NotEmpty(t, cs.Honours)
	require.Len(t, cs.MatrixPlayers, 3) // Sam, Kit, Pat

	// Guess Matrix check
	require.NotEmpty(t, cs.GuessMatrix)
}

func TestPlayerProfile(t *testing.T) {
	statsStore, _, club, _, _, _, pat := setupClosedRound(t)
	ctx := context.Background()

	prof, err := statsStore.PlayerProfile(ctx, club.ID, pat.ID)
	require.NoError(t, err)

	require.Equal(t, pat.ID, prof.Player.ID)
	require.Equal(t, 1, prof.RoundsPlayed)
	require.Equal(t, 1, prof.Honours.BestFitWins)
	require.Len(t, prof.Songs, 1)
	require.Equal(t, "Bowie", prof.Songs[0].Artist)
}
