package web

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCountdown(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{-time.Hour, "deadline passed"},
		{0, "deadline passed"},
		{30 * time.Second, "less than a minute left"},
		{time.Minute, "1 minute left"},
		{45 * time.Minute, "45 minutes left"},
		{time.Hour, "1 hour left"},
		{47 * time.Hour, "47 hours left"},
		{48 * time.Hour, "2 days left"},
		{14*24*time.Hour - time.Minute, "13 days left"},
	} {
		require.Equal(t, tc.want, countdown(now, now.Add(tc.in)), tc.in.String())
	}
}
