package linkresolver

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memCache map[string][]Match

func (c memCache) Get(_ context.Context, provider, key string, _ time.Duration) ([]Match, error) {
	return c[provider+"|"+key], nil
}

func (c memCache) Set(_ context.Context, provider, key string, m []Match) error {
	c[provider+"|"+key] = m
	return nil
}

type fakeProvider struct {
	results []Match
	err     error
	calls   int
}

func (p *fakeProvider) Search(context.Context, string, string) ([]Match, error) {
	p.calls++
	return p.results, p.err
}

func TestCandidatesAreCachedAndCapped(t *testing.T) {
	var many []Match
	for i := range 8 {
		many = append(many, Match{ExternalID: fmt.Sprint(i), Title: "Bohemian Rhapsody", Artist: "Queen"})
	}
	p := &fakeProvider{results: many}
	r := NewResolver(memCache{}, time.Hour, p)
	ctx := context.Background()

	got, err := r.Candidates(ctx, "Queen", "Bohemian Rhapsody")
	require.NoError(t, err)
	require.Len(t, got, maxCandidates)
	require.Equal(t, 1, p.calls)

	// Typed differently but normalising to the same key: served from the cache.
	again, err := r.Candidates(ctx, "QUEEN", "bohemian rhapsody (Remastered 2011)")
	require.NoError(t, err)
	require.Equal(t, got, again)
	require.Equal(t, 1, p.calls, "provider calls")
}

func TestCandidatesWithoutAProviderSuggestNothing(t *testing.T) {
	got, err := NewResolver(memCache{}, time.Hour, nil).Candidates(context.Background(), "Queen", "Radio Ga Ga")
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCandidatesSkipUnsearchableInput(t *testing.T) {
	p := &fakeProvider{}
	got, err := NewResolver(memCache{}, time.Hour, p).Candidates(context.Background(), "!!!", "???")
	require.NoError(t, err)
	require.Empty(t, got)
	require.Zero(t, p.calls)
}

func TestCandidatesProviderErrorIsReturnedAndNotCached(t *testing.T) {
	cache := memCache{}
	p := &fakeProvider{err: errors.New("quota")}
	r := NewResolver(cache, time.Hour, p)

	_, err := r.Candidates(context.Background(), "Queen", "Radio Ga Ga")
	require.Error(t, err)
	require.Empty(t, cache)
}
