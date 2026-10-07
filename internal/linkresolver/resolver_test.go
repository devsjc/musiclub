package linkresolver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"musiclub/internal/domain"
)

type mockStore struct {
	cache map[string][]Match
	links []domain.SubmissionLink
}

func newMockStore() *mockStore {
	return &mockStore{cache: make(map[string][]Match)}
}

func (m *mockStore) GetLinkCache(ctx context.Context, provider, queryKey string, maxAge time.Duration) ([]Match, error) {
	return m.cache[provider+":"+queryKey], nil
}

func (m *mockStore) SetLinkCache(ctx context.Context, provider, queryKey string, matches []Match) error {
	m.cache[provider+":"+queryKey] = matches
	return nil
}

func (m *mockStore) AddSubmissionLink(ctx context.Context, submissionID int64, service, url, externalID string) error {
	m.links = append(m.links, domain.SubmissionLink{
		SubmissionID: submissionID,
		Service:      service,
		URL:          url,
		ExternalID:   externalID,
	})
	return nil
}

func (m *mockStore) GetSubmissionLinks(ctx context.Context, submissionID int64) ([]domain.SubmissionLink, error) {
	return m.links, nil
}

type mockProvider struct {
	service string
	calls   int
	results []Match
}

func (p *mockProvider) Service() string { return p.service }
func (p *mockProvider) Search(ctx context.Context, artist, title string) ([]Match, error) {
	p.calls++
	return p.results, nil
}

func TestResolverCandidates(t *testing.T) {
	store := newMockStore()
	yt := &mockProvider{
		service: YouTubeMusic,
		results: []Match{
			{Service: YouTubeMusic, Title: "Bohemian Rhapsody", Artist: "Queen", URL: "https://music.youtube.com/watch?v=1"},
			{Service: YouTubeMusic, Title: "Bohemian Rhapsody (Live)", Artist: "Queen", URL: "https://music.youtube.com/watch?v=2"},
		},
	}

	r := NewResolver(store, time.Hour, yt)
	ctx := context.Background()

	// First call: calls provider
	matches, err := r.Candidates(ctx, "Queen", "Bohemian Rhapsody")
	require.NoError(t, err)
	require.Len(t, matches, 2)
	require.Equal(t, 1, yt.calls)

	// Second call: served from cache, provider not called again
	matches2, err := r.Candidates(ctx, "Queen", "Bohemian Rhapsody")
	require.NoError(t, err)
	require.Len(t, matches2, 2)
	require.Equal(t, 1, yt.calls)
}

func TestResolverEnrichHighConfidenceOnly(t *testing.T) {
	store := newMockStore()
	sp := &mockProvider{
		service: Spotify,
		results: []Match{
			// Wrong song (different title)
			{Service: Spotify, Title: "Another One Bites The Dust", Artist: "Queen", URL: "https://open.spotify.com/track/wrong"},
			// Exact match (normalised artist + title equals)
			{Service: Spotify, Title: "Bohemian Rhapsody", Artist: "Queen", URL: "https://open.spotify.com/track/exact", ExternalID: "exact123"},
		},
	}

	r := NewResolver(store, time.Hour, nil, sp)
	ctx := context.Background()

	require.NoError(t, r.Enrich(ctx, 42, "Queen", "Bohemian Rhapsody"))

	// Exact match saved to submission_links
	links, err := store.GetSubmissionLinks(ctx, 42)
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, "spotify", links[0].Service)
	require.Equal(t, "https://open.spotify.com/track/exact", links[0].URL)
	require.Equal(t, "exact123", links[0].ExternalID)
}
