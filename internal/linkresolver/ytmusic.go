package linkresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/prettyirrelevant/ytmusicapi"
)

// cacheProvider keys link_cache rows written by YTMusicProvider.
const cacheProvider = "ytmusic"

const authCookie = "__Secure-3PAPISID"

// MatchURLPrefix starts every link YTMusicProvider suggests.
const MatchURLPrefix = "https://music.youtube.com/watch?v="

// SetupYTMusic initialises the ytmusicapi client from the browser headers JSON
// described in its README (the YTMUSIC_HEADERS_JSON value). The library panics
// on bad input, so the headers are checked here first and returned as an error.
func SetupYTMusic(headersJSON string) error {
	normalized, err := normalizeHeaders(headersJSON)
	if err != nil {
		return err
	}
	if err := os.Setenv("YTMUSIC_HEADERS_JSON", normalized); err != nil {
		return err
	}
	ytmusicapi.Setup()
	return nil
}

// unsafeHeaders are written by ytmusicapi's setup tool but break its requests.
// Setting accept-encoding by hand turns off Go's transparent gzip handling, so
// the compressed reply reaches ytmusicapi as bytes it cannot parse and every
// search quietly comes back empty. content-encoding claims a compressed body
// when ytmusicapi sends plain JSON.
var unsafeHeaders = []string{"accept-encoding", "content-encoding"}

// normalizeHeaders prepares the headers for ytmusicapi. It checks they carry
// the cookie ytmusicapi signs its requests with, and rewrites that cookie
// header with no space after each ";": ytmusicapi finds the cookie by splitting
// on ";" without trimming, so it would miss it unless it happened to come first.
// It also drops unsafeHeaders.
func normalizeHeaders(raw string) (string, error) {
	var headers map[string]string
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return "", fmt.Errorf("YTMUSIC_HEADERS_JSON is not a JSON object of strings: %w", err)
	}

	var cookies []string
	var found bool
	for _, part := range strings.Split(headers["cookie"], ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		found = found || strings.HasPrefix(part, authCookie+"=")
		cookies = append(cookies, part)
	}
	if !found {
		return "", fmt.Errorf("YTMUSIC_HEADERS_JSON has no %s in its cookie header", authCookie)
	}
	headers["cookie"] = strings.Join(cookies, ";")
	for name := range headers {
		if slices.ContainsFunc(unsafeHeaders, func(h string) bool { return strings.EqualFold(h, name) }) {
			delete(headers, name)
		}
	}

	out, err := json.Marshal(headers)
	return string(out), err
}

// YTMusicProvider finds songs through the unofficial YouTube Music API.
// SetupYTMusic must have succeeded first.
type YTMusicProvider struct{}

func NewYTMusicProvider() *YTMusicProvider { return &YTMusicProvider{} }

// Search returns the top song results for the artist and title. ytmusicapi
// takes no context and can panic on responses it does not expect, so the call
// runs in its own goroutine: ctx can abandon it, and a panic becomes an error.
func (p *YTMusicProvider) Search(ctx context.Context, artist, title string) ([]Match, error) {
	type outcome struct {
		matches []Match
		err     error
	}
	done := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- outcome{err: fmt.Errorf("ytmusic search: %v", r)}
			}
		}()
		results, err := ytmusicapi.SearchTracks(artist+" "+title, ytmusicapi.SongsFilter, ytmusicapi.NoScope, maxCandidates, false)
		done <- outcome{matches: toMatches(results), err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case o := <-done:
		return o.matches, o.err
	}
}

// toMatches keeps the playable songs from a search.
func toMatches(results []ytmusicapi.SearchResult) []Match {
	var matches []Match
	for _, r := range results {
		if r.ResultType != "song" || r.VideoID == "" {
			continue
		}
		matches = append(matches, Match{
			ExternalID: r.VideoID,
			URL:        MatchURLPrefix + url.QueryEscape(r.VideoID),
			Title:      r.Title,
			Artist:     strings.Join(r.Artistes, ", "),
		})
	}
	return matches
}

var _ Provider = (*YTMusicProvider)(nil)
