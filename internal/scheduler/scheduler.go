package scheduler

import (
	"context"
	"log/slog"
	"time"

	"musiclub/internal/rounds"
)

// Run runs the ticker that advances round phases for all clubs.
// It blocks until ctx is canceled.
func Run(ctx context.Context, svc *rounds.Service, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Info("scheduler started", "interval", interval)
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := svc.Tick(ctx, now); err != nil {
				slog.Error("scheduler tick failed", "error", err)
			}
		}
	}
}
