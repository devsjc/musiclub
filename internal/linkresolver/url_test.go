package linkresolver

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCleanManualURL(t *testing.T) {
	for _, c := range []struct {
		in, want, service string
	}{
		{"https://open.spotify.com/track/abc123?si=xyz", "https://open.spotify.com/track/abc123", Spotify},
		{"  https://music.youtube.com/watch?v=abc&si=xyz  ", "https://music.youtube.com/watch?v=abc", YouTubeMusic},
		{"https://www.youtube.com/watch?v=abc&feature=share&t=30", "https://www.youtube.com/watch?t=30&v=abc", YouTubeMusic},
		{"https://youtu.be/abc?si=xyz", "https://youtu.be/abc", YouTubeMusic},
		{"HTTPS://Play.Qobuz.com/album/1", "https://play.qobuz.com/album/1", Qobuz},
		{"https://artist.bandcamp.com/track/x?utm_source=a&utm_medium=b&from=embed", "https://artist.bandcamp.com/track/x?from=embed", Other},
		{"http://soundcloud.com/a/b#t=10", "http://soundcloud.com/a/b", Other},
		{"https://example.com/a%20b?q=1", "https://example.com/a%20b?q=1", Other},
	} {
		got, service, err := CleanManualURL(c.in)
		require.NoError(t, err, "CleanManualURL(%q)", c.in)
		require.Equal(t, c.want, got, "CleanManualURL(%q)", c.in)
		require.Equal(t, c.service, service, "CleanManualURL(%q) service", c.in)
	}
}

func TestCleanManualURLRejects(t *testing.T) {
	for _, c := range []struct {
		in   string
		want error
	}{
		{"", ErrURLRequired},
		{"   ", ErrURLRequired},
		{"javascript:alert(1)", ErrURLInvalid},
		{"JavaScript://example.com/%0Aalert(1)", ErrURLInvalid},
		{"data:text/html,<script>alert(1)</script>", ErrURLInvalid},
		{"ftp://example.com/song.mp3", ErrURLInvalid},
		{"file:///etc/passwd", ErrURLInvalid},
		{"//example.com/x", ErrURLInvalid},
		{"example.com/song", ErrURLInvalid},
		{"https://", ErrURLInvalid},
		{"https:///path", ErrURLInvalid},
		{"https://user:pass@example.com/x", ErrURLInvalid},
		{"https:example.com", ErrURLInvalid},
		{"https://exa mple.com", ErrURLInvalid},
		{"https://example.com/" + strings.Repeat("a", MaxURLLength), ErrURLTooLong},
	} {
		_, _, err := CleanManualURL(c.in)
		require.ErrorIs(t, err, c.want, "CleanManualURL(%.40q)", c.in)
	}
}
