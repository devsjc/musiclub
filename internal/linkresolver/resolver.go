package linkresolver

import (
	"context"
	"time"

	"golang.org/x/sync/semaphore"
	"musiclub/internal/domain"
	"musiclub/internal/songkey"
)

type Match struct {
	Service    string        `json:"service"`
	ExternalID string        `json:"external_id"`
	URL        string        `json:"url"`
	Title      string        `json:"title"`
	Artist     string        `json:"artist"`
	Album      string        `json:"album"`
	Duration   time.Duration `json:"duration"`
	ThumbURL   string        `json:"thumb_url"`
}

type Provider interface {
	Service() string
	Search(ctx context.Context, artist, title string) ([]Match, error)
}

type LinkStore interface {
	GetLinkCache(ctx context.Context, provider, queryKey string, maxAge time.Duration) ([]Match, error)
	SetLinkCache(ctx context.Context, provider, queryKey string, matches []Match) error
	AddSubmissionLink(ctx context.Context, submissionID int64, service, url, externalID string) error
	GetSubmissionLinks(ctx context.Context, submissionID int64) ([]domain.SubmissionLink, error)
}

type Resolver struct {
	primary Provider
	extras  []Provider
	store   LinkStore
	ttl     time.Duration
	limiter *semaphore.Weighted
}

func NewResolver(store LinkStore, cacheTTL time.Duration, primary Provider, extras ...Provider) *Resolver {
	return &Resolver{
		primary: primary,
		extras:  extras,
		store:   store,
		ttl:     cacheTTL,
		limiter: semaphore.NewWeighted(10), // Small token bucket for outbound calls
	}
}

func (r *Resolver) Candidates(ctx context.Context, artist, title string) ([]Match, error) {
	if r.primary == nil {
		return nil, nil // No provider, no candidates
	}

	queryKey := songkey.Key(artist, title)
	if queryKey == "" {
		return nil, nil
	}

	providerName := r.primary.Service()

	// Cache check
	cached, err := r.store.GetLinkCache(ctx, providerName, queryKey, r.ttl)
	if err == nil && cached != nil {
		return cached, nil
	}

	// Rate limit
	if err := r.limiter.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer r.limiter.Release(1)

	// Timeout for Candidates (3s)
	searchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	matches, err := r.primary.Search(searchCtx, artist, title)
	if err != nil {
		return nil, err
	}

	if len(matches) > 5 {
		matches = matches[:5]
	}

	// Save to cache asynchronously or synchronously (synchronously is fine, it's quick)
	_ = r.store.SetLinkCache(ctx, providerName, queryKey, matches)

	return matches, nil
}

func (r *Resolver) Enrich(ctx context.Context, submissionID int64, artist, title string) error {
	if len(r.extras) == 0 {
		return nil
	}

	queryKey := songkey.Key(artist, title)
	if queryKey == "" {
		return nil
	}

	for _, p := range r.extras {
		// Cache check
		cached, err := r.store.GetLinkCache(ctx, p.Service(), queryKey, r.ttl)
		var matches []Match

		if err == nil && cached != nil {
			matches = cached
		} else {
			if err := r.limiter.Acquire(ctx, 1); err != nil {
				continue
			}
			searchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			m, err := p.Search(searchCtx, artist, title)
			cancel()
			r.limiter.Release(1)

			if err == nil {
				matches = m
				_ = r.store.SetLinkCache(ctx, p.Service(), queryKey, matches)
			}
		}

		// Find a high-confidence match
		for _, m := range matches {
			if songkey.Key(m.Artist, m.Title) == queryKey {
				_ = r.store.AddSubmissionLink(ctx, submissionID, p.Service(), m.URL, m.ExternalID)
				break // saved
			}
		}
	}
	return nil
}
