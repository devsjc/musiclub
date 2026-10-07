package linkresolver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type SpotifyProvider struct {
	clientID     string
	clientSecret string
	client       *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func NewSpotifyProvider(clientID, clientSecret string) *SpotifyProvider {
	return &SpotifyProvider{
		clientID:     clientID,
		clientSecret: clientSecret,
		client:       &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *SpotifyProvider) Service() string {
	return Spotify
}

func (p *SpotifyProvider) getToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && time.Now().Before(p.expiresAt) {
		return p.token, nil
	}

	auth := base64.StdEncoding.EncodeToString([]byte(p.clientID + ":" + p.clientSecret))
	data := url.Values{}
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://accounts.spotify.com/api/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("spotify token api returned status: %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", err
	}

	p.token = tokenResp.AccessToken
	p.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second) // 1 min buffer
	return p.token, nil
}

func (p *SpotifyProvider) Search(ctx context.Context, artist, title string) ([]Match, error) {
	if p.clientID == "" || p.clientSecret == "" {
		return nil, nil
	}

	token, err := p.getToken(ctx)
	if err != nil {
		return nil, err
	}

	q := fmt.Sprintf("track:%s artist:%s", title, artist)
	reqURL := fmt.Sprintf("https://api.spotify.com/v1/search?type=track&limit=5&q=%s", url.QueryEscape(q))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("spotify search api returned status: %d", resp.StatusCode)
	}

	var searchResp struct {
		Tracks struct {
			Items []struct {
				Id      string `json:"id"`
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Name   string `json:"name"`
					Images []struct {
						Url string `json:"url"`
					} `json:"images"`
				} `json:"album"`
				DurationMs   int `json:"duration_ms"`
				ExternalUrls struct {
					Spotify string `json:"spotify"`
				} `json:"external_urls"`
			} `json:"items"`
		} `json:"tracks"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, err
	}

	var matches []Match
	for _, item := range searchResp.Tracks.Items {
		var artistName string
		if len(item.Artists) > 0 {
			artistName = item.Artists[0].Name
		}

		var thumb string
		if len(item.Album.Images) > 0 {
			thumb = item.Album.Images[len(item.Album.Images)-1].Url // Use smallest image
		}

		matches = append(matches, Match{
			Service:    Spotify,
			ExternalID: item.Id,
			URL:        item.ExternalUrls.Spotify,
			Title:      item.Name,
			Artist:     artistName,
			Album:      item.Album.Name,
			Duration:   time.Duration(item.DurationMs) * time.Millisecond,
			ThumbURL:   thumb,
		})
	}

	return matches, nil
}
