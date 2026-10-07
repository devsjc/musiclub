// Package config parses environment variables into a Config.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port                string
	DBPath              string
	CookieSecret        string
	Location            *time.Location // deadlines are interpreted in this zone
	YouTubeAPIKey       string
	SpotifyClientID     string
	SpotifyClientSecret string
	LinkCacheTTL        time.Duration
}

// Load reads configuration from the environment. It does not require
// COOKIE_SECRET, since one-off commands such as `club add` don't use it;
// call RequireSecret before starting the server.
func Load() (Config, error) {
	ttlStr := getenv("LINK_CACHE_TTL", "168h") // default 7 days
	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		return Config{}, fmt.Errorf("LINK_CACHE_TTL %q: %w", ttlStr, err)
	}

	cfg := Config{
		Port:                getenv("PORT", "8080"),
		DBPath:              getenv("DB_PATH", "/data/club.db"),
		CookieSecret:        os.Getenv("COOKIE_SECRET"),
		YouTubeAPIKey:       os.Getenv("YOUTUBE_API_KEY"),
		SpotifyClientID:     os.Getenv("SPOTIFY_CLIENT_ID"),
		SpotifyClientSecret: os.Getenv("SPOTIFY_CLIENT_SECRET"),
		LinkCacheTTL:        ttl,
	}

	if _, err := strconv.ParseUint(cfg.Port, 10, 16); err != nil {
		return Config{}, fmt.Errorf("PORT %q is not a valid port", cfg.Port)
	}

	tz := getenv("TIMEZONE", "Europe/London")
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Config{}, fmt.Errorf("TIMEZONE %q: %w", tz, err)
	}
	cfg.Location = loc

	return cfg, nil
}

// RequireSecret reports an error if COOKIE_SECRET is unset.
func (c Config) RequireSecret() error {
	if c.CookieSecret == "" {
		return errors.New("COOKIE_SECRET is required")
	}
	return nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
