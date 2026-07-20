package cron

import (
	"time"

	"github.com/sirupsen/logrus"
)

// Cron is a recurring trigger declaration: Every for intervals, or Daily
// ("15:04", local time) for once-a-day. Config-defined crons need no
// durability — they reappear from brain code on startup.
type Cron struct {
	Every    time.Duration
	Daily    string
	Pipeline string
}

// Next returns c's next firing time after now.
// ponytail: Every + Daily cover the reference stories; a cron-expression
// library slots in here if a brain ever needs one.
func Next(c Cron, now time.Time) time.Time {
	if c.Every > 0 {
		return now.Add(c.Every)
	}
	t, err := time.ParseInLocation("15:04", c.Daily, now.Location())
	if err != nil {
		logrus.WithField("daily", c.Daily).Error("invalid daily schedule; firing in 24h")
		return now.Add(24 * time.Hour)
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
