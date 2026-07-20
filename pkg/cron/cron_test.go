package cron

import (
	"testing"
	"time"
)

func TestNext(t *testing.T) {
	now := time.Date(2026, 7, 19, 20, 0, 0, 0, time.UTC)

	if got := Next(Cron{Every: 5 * time.Minute}, now); !got.Equal(now.Add(5 * time.Minute)) {
		t.Fatalf("every: %v", got)
	}
	// daily later today
	if got := Next(Cron{Daily: "21:00"}, now); !got.Equal(time.Date(2026, 7, 19, 21, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily today: %v", got)
	}
	// daily already passed → tomorrow
	if got := Next(Cron{Daily: "19:00"}, now); !got.Equal(time.Date(2026, 7, 20, 19, 0, 0, 0, time.UTC)) {
		t.Fatalf("daily tomorrow: %v", got)
	}
	// invalid spec falls back to +24h instead of spinning
	if got := Next(Cron{Daily: "not-a-time"}, now); !got.Equal(now.Add(24 * time.Hour)) {
		t.Fatalf("invalid: %v", got)
	}
}
