package rounds

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"musiclub/internal/domain"
	"musiclub/internal/linkresolver"
	"musiclub/internal/songkey"
	"musiclub/internal/store"
)

const (
	maxSongField = 200
	maxNote      = 500
)

var (
	ErrArtistRequired = errors.New("an artist is required")
	ErrTitleRequired  = errors.New("a title is required")
	ErrSongFieldLong  = fmt.Errorf("the artist and title must each be at most %d characters", maxSongField)
	ErrSongUnreadable = errors.New("the artist and title must contain letters or numbers")
	ErrNoteTooLong    = fmt.Errorf("the note must be at most %d characters", maxNote)
	ErrMatchedLink    = errors.New("that suggested link is not a YouTube Music link")
)

// Song is a player's submission. The link is either one they pasted ("manual")
// or a suggestion they confirmed ("matched", DESIGN.md §5.5).
type Song struct {
	Artist, Title string
	URL           string
	Note          string
	LinkSource    string // "manual" or "matched"
	ExternalID    string // the suggestion's video ID, for matched links
}

// SuggestLinks returns candidate listening links for an artist and title.
func (s *Service) SuggestLinks(ctx context.Context, artist, title string) ([]linkresolver.Match, error) {
	if s.resolver == nil {
		return nil, nil
	}
	return s.resolver.Candidates(ctx, artist, title)
}

// SubmitSong saves the player's song for the round.
// It validates the input, cleans the link and computes the duplicate key. The
// round must be submitting and before its deadline.
func (s *Service) SubmitSong(ctx context.Context, clubID, roundID, playerID int64, song Song) (domain.Submission, error) {
	artist, title, note := strings.TrimSpace(song.Artist), strings.TrimSpace(song.Title), strings.TrimSpace(song.Note)
	switch {
	case artist == "":
		return domain.Submission{}, ErrArtistRequired
	case title == "":
		return domain.Submission{}, ErrTitleRequired
	case utf8.RuneCountInString(artist) > maxSongField || utf8.RuneCountInString(title) > maxSongField:
		return domain.Submission{}, ErrSongFieldLong
	case utf8.RuneCountInString(note) > maxNote:
		return domain.Submission{}, ErrNoteTooLong
	}
	key := songkey.Key(artist, title)
	if key == "" {
		return domain.Submission{}, ErrSongUnreadable
	}

	finalURL, service, err := linkresolver.CleanManualURL(song.URL)
	if err != nil {
		return domain.Submission{}, err
	}
	source, externalID := "manual", ""
	if song.LinkSource == "matched" {
		// Suggestions round-trip through the browser, so check they really are ours.
		if !strings.HasPrefix(finalURL, linkresolver.MatchURLPrefix) {
			return domain.Submission{}, ErrMatchedLink
		}
		source, externalID = "matched", song.ExternalID
	}

	return s.subs.Create(ctx, clubID, roundID, playerID, store.Song{
		Artist: artist, Title: title, URL: finalURL, URLService: service, LinkSource: source, ExternalID: externalID, Note: note, SongKey: key,
	}, s.now())
}

// DeleteSubmission removes a submission by ID.
func (s *Service) DeleteSubmission(ctx context.Context, clubID, roundID, submissionID int64) error {
	return s.subs.Delete(ctx, clubID, roundID, submissionID)
}

// ListSubmissions returns all submissions for a round.
func (s *Service) ListSubmissions(ctx context.Context, clubID, roundID int64) ([]domain.Submission, error) {
	return s.subs.List(ctx, clubID, roundID)
}
