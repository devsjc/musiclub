package linkresolver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type YouTubeProvider struct {
	apiKey string
	client *http.Client
}

func NewYouTubeProvider(apiKey string) *YouTubeProvider {
	return &YouTubeProvider{
		apiKey: apiKey,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *YouTubeProvider) Service() string {
	return YouTubeMusic
}

func (p *YouTubeProvider) Search(ctx context.Context, artist, title string) ([]Match, error) {
	if p.apiKey == "" {
		return nil, nil
	}

	q := fmt.Sprintf("%s %s", artist, title)
	
	reqURL := fmt.Sprintf("https://www.googleapis.com/youtube/v3/search?part=snippet&type=video&videoCategoryId=10&maxResults=5&q=%s&key=%s", 
		url.QueryEscape(q), url.QueryEscape(p.apiKey))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("youtube api returned status: %d", resp.StatusCode)
	}

	var ytResp struct {
		Items []struct {
			Id struct {
				VideoId string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title        string `json:"title"`
				ChannelTitle string `json:"channelTitle"`
				Thumbnails   struct {
					Default struct {
						Url string `json:"url"`
					} `json:"default"`
				} `json:"thumbnails"`
			} `json:"snippet"`
		} `json:"items"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ytResp); err != nil {
		return nil, err
	}

	var matches []Match
	for _, item := range ytResp.Items {
		if item.Id.VideoId == "" {
			continue
		}

		// ChannelTitle is used as a proxy for Artist, which isn't perfect for YouTube but good enough for candidates
		artistName := item.Snippet.ChannelTitle

		// Some cleanup of common YouTube suffixes
		if len(artistName) > 8 && artistName[len(artistName)-8:] == " - Topic" {
			artistName = artistName[:len(artistName)-8]
		}
		
		matches = append(matches, Match{
			Service:    YouTubeMusic,
			ExternalID: item.Id.VideoId,
			URL:        fmt.Sprintf("https://music.youtube.com/watch?v=%s", item.Id.VideoId),
			Title:      item.Snippet.Title,
			Artist:     artistName,
			ThumbURL:   item.Snippet.Thumbnails.Default.Url,
		})
	}

	return matches, nil
}
