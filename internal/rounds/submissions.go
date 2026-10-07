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
)

// Song is a player's submission. The link is one the player pasted; matched
// links from a provider will arrive with link matching (DESIGN.md §5.5).
type Song struct {
	Artist, Title string
	URL           string
	Note          string
	LinkSource    string // "manual" or "matched"
	URLService    string
	ExternalID    string
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

	var finalURL, service, source, externalID string
	if song.LinkSource == "matched" {
		cleanedURL, detectedService, err := linkresolver.CleanManualURL(song.URL)
		if err != nil {
			return domain.Submission{}, err
		}
		finalURL = cleanedURL
		service = song.URLService
		if service == "" {
			service = detectedService
		}
		source = "matched"
		externalID = song.ExternalID
	} else {
		// Clean the manual URL
		var err error
		finalURL, service, err = linkresolver.CleanManualURL(song.URL)
		if err != nil {
			return domain.Submission{}, err
		}
		source = "manual"
	}

	sub, err := s.subs.Create(ctx, clubID, roundID, playerID, store.Song{
		Artist: artist, Title: title, URL: finalURL, URLService: service, LinkSource: source, ExternalID: externalID, Note: note, SongKey: key,
	}, s.now())
	if err != nil {
		return sub, err
	}

	// Enrich asynchronously if a resolver is configured
	if s.resolver != nil {
		go func() {
			// Using a background context because the request context will be cancelled
			// when the HTTP handler returns.
			_ = s.resolver.Enrich(context.Background(), sub.ID, sub.Artist, sub.Title)
		}()
	}

	return sub, nil
}

// DeleteSubmission removes a submission by ID.
func (s *Service) DeleteSubmission(ctx context.Context, clubID, roundID, submissionID int64) error {
	return s.subs.Delete(ctx, clubID, roundID, submissionID)
}

// ListSubmissions returns all submissions for a round.
func (s *Service) ListSubmissions(ctx context.Context, clubID, roundID int64) ([]domain.Submission, error) {
	return s.subs.List(ctx, clubID, roundID)
}
