package web

import (
	"fmt"
	"time"
)

// countdown describes how long is left until due, e.g. "3 days left" or
// "closing soon". It is computed on the server, so it is as fresh as the page.
func countdown(now, due time.Time) string {
	left := due.Sub(now)
	switch {
	case left <= 0:
		return "deadline passed"
	case left < time.Minute:
		return "less than a minute left"
	case left < time.Hour:
		return plural(int(left/time.Minute), "minute") + " left"
	case left < 48*time.Hour:
		return plural(int(left/time.Hour), "hour") + " left"
	default:
		return plural(int(left/(24*time.Hour)), "day") + " left"
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", unit)
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
