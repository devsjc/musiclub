// Package linkresolver deals with listening links (DESIGN.md §5.5). For now it
// only validates and classifies pasted URLs; provider search comes later.
package linkresolver

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// MaxURLLength caps stored links.
const MaxURLLength = 2000

var (
	ErrURLRequired = errors.New("a listening link is required")
	ErrURLTooLong  = fmt.Errorf("the link must be at most %d characters", MaxURLLength)
	ErrURLInvalid  = errors.New("the link must be a full http:// or https:// address")
)

// Service names match submissions.url_service.
const (
	YouTubeMusic = "youtube_music"
	Spotify      = "spotify"
	Qobuz        = "qobuz"
	Other        = "other"
)

var hostServices = map[string]string{
	"music.youtube.com": YouTubeMusic,
	"www.youtube.com":   YouTubeMusic,
	"youtube.com":       YouTubeMusic,
	"m.youtube.com":     YouTubeMusic,
	"youtu.be":          YouTubeMusic,
	"open.spotify.com":  Spotify,
	"play.qobuz.com":    Qobuz,
	"open.qobuz.com":    Qobuz,
	"www.qobuz.com":     Qobuz,
}

// trackingParams are dropped from stored links. Names ending in "_" match as prefixes.
var trackingParams = []string{"si", "feature", "context", "fbclid", "gclid", "igshid", "utm_"}

// CleanManualURL validates a link the player pasted and returns it in the form
// to store, along with the service it belongs to ("other" if not recognised).
// Any http(s) host is accepted; the link is never fetched (§5.5).
func CleanManualURL(raw string) (cleaned, service string, err error) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return "", "", ErrURLRequired
	case len(raw) > MaxURLLength:
		return "", "", ErrURLTooLong
	}

	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return "", "", ErrURLInvalid
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""

	service = Other
	if s, ok := hostServices[u.Hostname()]; ok {
		service = s
	}

	q := u.Query()
	for name := range q {
		if isTracking(strings.ToLower(name)) {
			q.Del(name)
		}
	}
	u.RawQuery = q.Encode()

	cleaned = u.String()
	if len(cleaned) > MaxURLLength {
		return "", "", ErrURLTooLong
	}
	return cleaned, service, nil
}

func isTracking(name string) bool {
	for _, p := range trackingParams {
		if name == p || (strings.HasSuffix(p, "_") && strings.HasPrefix(name, p)) {
			return true
		}
	}
	return false
}
