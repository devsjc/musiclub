package rounds

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
)

func reviewRound(t *testing.T) (*Service, domain.Club, domain.Round, domain.Player, domain.Player, domain.Player, []domain.Submission) {
	t.Helper()
	s, club, r, sam, kit, pat, subs, database := votingRound(t)
	ctx := context.Background()

	// Cast ballots so subs[1] wins favorite and subs[2] wins best fit
	// sam votes for subs[1] (fav) and subs[2] (best fit)
	formSam := url.Values{
		"favorite_submission_id":            {fmt.Sprintf("%d", subs[1].ID)},
		"favorite_comment":                  {"Sam loves Kit's song"},
		"best_fit_submission_id":            {fmt.Sprintf("%d", subs[2].ID)},
		"best_fit_comment":                  {"Sam thinks Pat fits best"},
		fmt.Sprintf("guess_%d", subs[1].ID): {fmt.Sprintf("%d", kit.ID)},
		fmt.Sprintf("guess_%d", subs[2].ID): {fmt.Sprintf("%d", pat.ID)},
	}
	require.NoError(t, s.SubmitBallot(ctx, club.ID, r.ID, sam.ID, formSam))

	// kit votes for subs[1] (fav) and subs[2] (best fit)
	formKit := url.Values{
		"favorite_submission_id":            {fmt.Sprintf("%d", subs[0].ID)},
		"favorite_comment":                  {"Kit likes Sam's song"},
		"best_fit_submission_id":            {fmt.Sprintf("%d", subs[2].ID)},
		"best_fit_comment":                  {"Kit thinks Pat fits best"},
		fmt.Sprintf("guess_%d", subs[0].ID): {fmt.Sprintf("%d", sam.ID)},
		fmt.Sprintf("guess_%d", subs[2].ID): {fmt.Sprintf("%d", pat.ID)},
	}
	require.NoError(t, s.SubmitBallot(ctx, club.ID, r.ID, kit.ID, formKit))

	// Advance to review
	_, err := database.Exec(`UPDATE rounds SET phase = 'review' WHERE id = ?`, r.ID)
	require.NoError(t, err)

	return s, club, r, sam, kit, pat, subs
}

func TestReviewDataAndQuotes(t *testing.T) {
	s, club, r, _, _, _, subs := reviewRound(t)
	ctx := context.Background()

	data, err := s.Review(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PhaseReview, data.Round.Phase)
	require.Len(t, data.CandidateQuotes, 4) // Sam's 2 comments + Kit's 2 comments

	// Attaching quote to non-winner fails
	err = s.AddFeaturedQuote(ctx, club.ID, r.ID, subs[0].ID, domain.QuoteCategoryBestFit, nil, "Not a winner")
	require.Error(t, err)

	// Attaching quote to winning submission succeeds
	err = s.AddFeaturedQuote(ctx, club.ID, r.ID, subs[2].ID, domain.QuoteCategoryBestFit, nil, "Pat fits best!")
	require.NoError(t, err)

	data, err = s.Review(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Len(t, data.FeaturedQuotes, 1)
	require.Equal(t, "Pat fits best!", data.FeaturedQuotes[0].Quote)

	// Deleting quote
	require.NoError(t, s.DeleteFeaturedQuote(ctx, club.ID, r.ID, data.FeaturedQuotes[0].ID))
	data, err = s.Review(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Empty(t, data.FeaturedQuotes)
}

func TestSetSongKey(t *testing.T) {
	s, club, r, _, _, _, subs := reviewRound(t)
	ctx := context.Background()

	require.NoError(t, s.SetSongKey(ctx, club.ID, r.ID, subs[0].ID, "merged_key"))
	updated, err := s.subs.ByPlayer(ctx, club.ID, r.ID, subs[0].PlayerID)
	require.NoError(t, err)
	require.Equal(t, "merged_key", updated.SongKey)
}

func TestPublishAndRecompute(t *testing.T) {
	s, club, r, _, _, _, subs := reviewRound(t)
	ctx := context.Background()

	// Publish without quotes fails unless forced
	err := s.Publish(ctx, club.ID, r.ID, false)
	require.ErrorIs(t, err, ErrQuotesNotCurated)

	// Publish with force=true succeeds
	err = s.Publish(ctx, club.ID, r.ID, true)
	require.NoError(t, err)

	closedRound, err := s.rounds.Get(ctx, club.ID, r.ID)
	require.NoError(t, err)
	require.Equal(t, domain.PhaseClosed, closedRound.Phase)
	require.NotNil(t, closedRound.ClosedAt)

	// Recompute on closed round succeeds
	require.NoError(t, s.Recompute(ctx, club.ID, r.ID))

	_ = subs
}
