package store

import "time"

// sqliteTime is the layout produced by SQLite's datetime('now'); values are UTC.
const sqliteTime = "2006-01-02 15:04:05"

func parseTime(s string) (time.Time, error) {
	return time.ParseInLocation(sqliteTime, s, time.UTC)
}
