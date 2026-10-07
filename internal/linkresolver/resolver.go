package linkresolver

import (
	"context"
	"time"

	"golang.org/x/sync/semaphore"

	"musiclub/internal/songkey"
)

const (
	maxCandidates  = 2
	searchTimeout  = 3 * time.Second
	maxConcurrency = 4
)

// Match is a candidate listening link for a song.
type Match struct {
	ExternalID string `json:"external_id"` // the service's video ID
	URL        string `json:"url"`
	Title      string `json:"title"`
	Artist     string `json:"artist"`
}

// Provider looks up listening links for a song. Search must respect ctx.
type Provider interface {
	Search(ctx context.Context, artist, title string) ([]Match, error)
}

// Cache stores provider responses (DESIGN.md §5.5).
type Cache interface {
	Get(ctx context.Context, provider, queryKey string, maxAge time.Duration) ([]Match, error)
	Set(ctx context.Context, provider, queryKey string, matches []Match) error
}

// Resolver suggests links for a typed artist and title. Without a provider it
// suggests nothing, so submitting falls back to pasting a URL.
type Resolver struct {
	provider Provider
	cache    Cache
	ttl      time.Duration
	limiter  *semaphore.Weighted
}

func NewResolver(cache Cache, cacheTTL time.Duration, provider Provider) *Resolver {
	return &Resolver{
		provider: provider,
		cache:    cache,
		ttl:      cacheTTL,
		limiter:  semaphore.NewWeighted(maxConcurrency),
	}
}

// Candidates returns up to five matches: from the cache if fresh, otherwise
// from the provider under a short timeout.
func (r *Resolver) Candidates(ctx context.Context, artist, title string) ([]Match, error) {
	if r.provider == nil {
		return nil, nil
	}
	queryKey := songkey.Key(artist, title)
	if queryKey == "" {
		return nil, nil
	}

	if cached, err := r.cache.Get(ctx, cacheProvider, queryKey, r.ttl); err == nil && cached != nil {
		return cached, nil
	}

	if err := r.limiter.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer r.limiter.Release(1)

	searchCtx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()
	matches, err := r.provider.Search(searchCtx, artist, title)
	if err != nil {
		return nil, err
	}
	if len(matches) > maxCandidates {
		matches = matches[:maxCandidates]
	}

	_ = r.cache.Set(ctx, cacheProvider, queryKey, matches)
	return matches, nil
}
