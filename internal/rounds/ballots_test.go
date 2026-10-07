package rounds

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
	"musiclub/internal/store"
)

func votingRound(t *testing.T) (*Service, domain.Club, domain.Round, domain.Player, domain.Player, domain.Player, []domain.Submission, *sql.DB) {
	t.Helper()
	s, club, sam, database := newService(t)
	ctx := context.Background()

	kit, err := store.NewPlayers(database).Create(ctx, club.ID, "kit", false)
	require.NoError(t, err)
	pat, err := store.NewPlayers(database).Create(ctx, club.ID, "pat", false)
	require.NoError(t, err)

	r, err := s.StartRound(ctx, club.ID, "2026-03")
	require.NoError(t, err)
	require.NoError(t, s.SetTheme(ctx, club.ID, r.ID, r.PickerID, "Rain"))

	sub1, err := s.SubmitSong(ctx, club.ID, r.ID, sam.ID, Song{Artist: "Queen", Title: "Song 1", URL: "https://example.com/1"})
	require.NoError(t, err)
	sub2, err := s.SubmitSong(ctx, club.ID, r.ID, kit.ID, Song{Artist: "Beatles", Title: "Song 2", URL: "https://example.com/2"})
	require.NoError(t, err)
	sub3, err := s.SubmitSong(ctx, club.ID, r.ID, pat.ID, Song{Artist: "Bowie", Title: "Song 3", URL: "https://example.com/3"})
	require.NoError(t, err)

	// Advance to voting phase
	voteDue := s.now().AddDate(0, 0, 14)
	_, err = database.Exec(`UPDATE rounds SET phase = 'voting', voting_due_at = ? WHERE id = ?`,
		voteDue.UTC().Format("2006-01-02 15:04:05"), r.ID)
	require.NoError(t, err)

	return s, club, r, sam, kit, pat, []domain.Submission{sub1, sub2, sub3}, database
}

func TestSubmitBallotValid(t *testing.T) {
	s, club, r, sam, kit, pat, subs, _ := votingRound(t)
	ctx := context.Background()

	form := url.Values{
		"favorite_submission_id":            {fmt.Sprintf("%d", subs[1].ID)}, // kit's song
		"favorite_comment":                  {"Great track"},
		"best_fit_submission_id":            {fmt.Sprintf("%d", subs[2].ID)}, // pat's song
		"best_fit_comment":                  {"Fits well"},
		fmt.Sprintf("guess_%d", subs[1].ID): {fmt.Sprintf("%d", kit.ID)},
		fmt.Sprintf("guess_%d", subs[2].ID): {fmt.Sprintf("%d", pat.ID)},
	}

	err := s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, form)
	require.NoError(t, err)

	b, err := s.ballots.ByVoter(ctx, club.ID, r.ID, sam.ID)
	require.NoError(t, err)
	require.Equal(t, sam.ID, b.VoterID)
	require.Equal(t, subs[1].ID, b.FavoriteSubmissionID)
	require.Equal(t, "Great track", b.FavoriteComment)
	require.Equal(t, subs[2].ID, b.BestFitSubmissionID)
	require.Equal(t, "Fits well", b.BestFitComment)
	require.Len(t, b.Guesses, 2)
}

func TestSubmitBallotValidation(t *testing.T) {
	s, club, r, sam, kit, pat, subs, database := votingRound(t)
	ctx := context.Background()

	validForm := func() url.Values {
		return url.Values{
			"favorite_submission_id":            {fmt.Sprintf("%d", subs[1].ID)},
			"best_fit_submission_id":            {fmt.Sprintf("%d", subs[2].ID)},
			fmt.Sprintf("guess_%d", subs[1].ID): {fmt.Sprintf("%d", kit.ID)},
			fmt.Sprintf("guess_%d", subs[2].ID): {fmt.Sprintf("%d", pat.ID)},
		}
	}

	// Favorite and best fit cannot be the same
	f := validForm()
	f.Set("best_fit_submission_id", fmt.Sprintf("%d", subs[1].ID))
	require.ErrorIs(t, s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, f), ErrInvalidBallot)

	// Cannot vote for own song as favorite
	f = validForm()
	f.Set("favorite_submission_id", fmt.Sprintf("%d", subs[0].ID))
	require.ErrorIs(t, s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, f), ErrInvalidBallot)

	// Cannot vote for own song as best fit
	f = validForm()
	f.Set("best_fit_submission_id", fmt.Sprintf("%d", subs[0].ID))
	require.ErrorIs(t, s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, f), ErrInvalidBallot)

	// Non-existent song ID
	f = validForm()
	f.Set("favorite_submission_id", "9999")
	require.ErrorIs(t, s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, f), ErrInvalidBallot)

	// Missing guess
	f = validForm()
	f.Del(fmt.Sprintf("guess_%d", subs[1].ID))
	require.ErrorIs(t, s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, f), ErrInvalidBallot)

	// Guessing oneself
	f = validForm()
	f.Set(fmt.Sprintf("guess_%d", subs[1].ID), fmt.Sprintf("%d", sam.ID))
	require.ErrorIs(t, s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, f), ErrInvalidBallot)

	// Duplicate player guessed
	f = validForm()
	f.Set(fmt.Sprintf("guess_%d", subs[2].ID), fmt.Sprintf("%d", kit.ID))
	require.ErrorIs(t, s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, f), ErrInvalidBallot)

	// Guessing a player who did not submit a song
	alex, err := store.NewPlayers(database).Create(ctx, club.ID, "alex", false)
	require.NoError(t, err)
	f = validForm()
	f.Set(fmt.Sprintf("guess_%d", subs[1].ID), fmt.Sprintf("%d", alex.ID))
	require.ErrorIs(t, s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, f), ErrInvalidBallot)
}

func TestSubmitBallotNonSubmitterVoter(t *testing.T) {
	s, club, r, sam, kit, pat, subs, database := votingRound(t)
	ctx := context.Background()

	// Player who did not submit a song votes
	alex, err := store.NewPlayers(database).Create(ctx, club.ID, "alex", false)
	require.NoError(t, err)

	// Non-submitter must guess all 3 submitters
	form := url.Values{
		"favorite_submission_id":            {fmt.Sprintf("%d", subs[0].ID)},
		"best_fit_submission_id":            {fmt.Sprintf("%d", subs[1].ID)},
		fmt.Sprintf("guess_%d", subs[0].ID): {fmt.Sprintf("%d", sam.ID)},
		fmt.Sprintf("guess_%d", subs[1].ID): {fmt.Sprintf("%d", kit.ID)},
		fmt.Sprintf("guess_%d", subs[2].ID): {fmt.Sprintf("%d", pat.ID)},
	}

	err = s.SubmitBallot(ctx, club.ID, r.ID, alex.ID, form)
	require.NoError(t, err)

	b, err := s.ballots.ByVoter(ctx, club.ID, r.ID, alex.ID)
	require.NoError(t, err)
	require.Equal(t, alex.ID, b.VoterID)
	require.Len(t, b.Guesses, 3)
}
