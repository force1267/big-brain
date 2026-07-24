package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// Catch-up fires the target once per tick missed while the process was down.
func TestCronCatchup(t *testing.T) {
	e, _ := New(nil, nil)
	Register(e, "job", func(context.Context, struct{}) error { return nil })
	sc, _ := parseCron("* * * * *") // every minute
	now := time.Date(2026, 7, 24, 10, 5, 0, 0, time.UTC)
	e.now = func() time.Time { return now }
	scheduled := now.Add(-3 * time.Minute) // 10:02
	raw, _ := json.Marshal(scheduled)

	if err := e.fire(context.Background(), "job", struct{}{}, sc, cronCfg{catchup: true}, raw); err != nil {
		t.Fatal(err)
	}
	// ticks 10:02, 10:03, 10:04, 10:05 → 4 target runs
	n := 0
	for _, p := range e.pending {
		if p.Flow == "job" {
			n++
		}
	}
	if n != 4 {
		t.Fatalf("catch-up enqueued %d target runs, want 4", n)
	}

	// Without catch-up, a single fire enqueues exactly one.
	e2, _ := New(nil, nil)
	Register(e2, "job", func(context.Context, struct{}) error { return nil })
	e2.now = func() time.Time { return now }
	if err := e2.fire(context.Background(), "job", struct{}{}, sc, cronCfg{}, raw); err != nil {
		t.Fatal(err)
	}
	if len(e2.pending) != 1 {
		t.Fatalf("no-catchup fire enqueued %d, want 1", len(e2.pending))
	}
}

// Cancelling a one-shot writes no tombstone (bounded growth); a cron ticker does.
func TestCancelTombstoneBounded(t *testing.T) {
	st := NewMemStore()
	e, _ := New(st, nil)

	id, _ := e.Enqueue(context.Background(), "x", nil, time.Now().Add(time.Hour))
	if err := e.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Get(context.Background(), "cancelled/"+id); ok {
		t.Fatal("one-shot cancel should write no tombstone")
	}

	if err := e.Cancel(context.Background(), "tick/cron:* * * * *:job"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Get(context.Background(), "cancelled/tick/cron:* * * * *:job"); !ok {
		t.Fatal("ticker cancel should write a tombstone")
	}
}

func TestParseCronErrors(t *testing.T) {
	for _, spec := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "*/0 * * * *", "a * * * *"} {
		if _, err := parseCron(spec); !errors.Is(err, ErrCronSpec) {
			t.Errorf("parseCron(%q) = %v, want ErrCronSpec", spec, err)
		}
	}
}

func TestCronNext(t *testing.T) {
	// "30 9 * * *" — every day at 09:30.
	s, err := parseCron("30 9 * * *")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 7, 23, 9, 0, 0, 0, time.UTC)
	got := s.next(from)
	want := time.Date(2026, 7, 23, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next = %v, want %v", got, want)
	}
	// From just after 09:30 rolls to tomorrow.
	got = s.next(time.Date(2026, 7, 23, 9, 30, 0, 0, time.UTC))
	want = time.Date(2026, 7, 24, 9, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("next(after) = %v, want %v", got, want)
	}
}

func TestCronStepAndList(t *testing.T) {
	// "*/15 * * * *" matches minutes 0,15,30,45.
	s, _ := parseCron("*/15 * * * *")
	for _, m := range []int{0, 15, 30, 45} {
		if s.min&(1<<uint(m)) == 0 {
			t.Errorf("minute %d should match", m)
		}
	}
	if s.min&(1<<7) != 0 {
		t.Error("minute 7 should not match")
	}
}

// Every arms a durable ticker; running the tick enqueues the target flow and
// re-arms the ticker for the next occurrence. Driven synchronously so it does
// not depend on real-time dispatch.
func TestEveryFires(t *testing.T) {
	e, _ := New(nil, nil)
	e.now = func() time.Time { return time.Date(2026, 7, 23, 9, 29, 59, 0, time.UTC) }
	Register(e, "job", func(context.Context, struct{}) error { return nil })
	if _, err := Every(e, "30 9 * * *", "job", struct{}{}); err != nil {
		t.Fatal(err)
	}

	// One ticker run pending, armed for 09:30 today.
	if len(e.pending) != 1 {
		t.Fatalf("after Every: pending = %d, want 1 (ticker)", len(e.pending))
	}
	tick := e.pending[0]
	want := time.Date(2026, 7, 23, 9, 30, 0, 0, time.UTC)
	if !tick.Wake.Equal(want) {
		t.Fatalf("ticker armed for %v, want %v", tick.Wake, want)
	}

	// Run the tick (clock now just past 09:30): it should enqueue "job" and
	// re-arm itself for the next occurrence, tomorrow.
	e.pending = nil
	e.now = func() time.Time { return time.Date(2026, 7, 23, 9, 30, 30, 0, time.UTC) }
	if _, err := e.invoke(context.Background(), tick, e.flows[tick.Flow]); err != nil {
		t.Fatal(err)
	}
	var sawJob, sawTick bool
	for _, p := range e.pending {
		switch p.Flow {
		case "job":
			sawJob = true
		case tick.Flow:
			sawTick = true
			if nextDay := want.AddDate(0, 0, 1); !p.Wake.Equal(nextDay) {
				t.Errorf("re-armed for %v, want %v", p.Wake, nextDay)
			}
		}
	}
	if !sawJob {
		t.Error("tick did not enqueue the target flow")
	}
	if !sawTick {
		t.Error("tick did not re-arm itself")
	}
}

// Scheduled lists pending runs; a cron ticker carries its spec.
func TestScheduledLists(t *testing.T) {
	e, _ := New(nil, nil)
	Register(e, "job", func(context.Context, struct{}) error { return nil })
	id, err := Every(e, "0 21 * * *", "job", struct{}{})
	if err != nil {
		t.Fatal(err)
	}
	e.Enqueue(context.Background(), "job", struct{}{}, time.Now().Add(time.Hour))

	list := e.Scheduled()
	if len(list) != 2 {
		t.Fatalf("Scheduled = %d entries, want 2", len(list))
	}
	var foundCron bool
	for _, s := range list {
		if s.ID == id {
			foundCron = true
			if s.Cron != "0 21 * * *" {
				t.Errorf("ticker Cron = %q, want the spec", s.Cron)
			}
		}
	}
	if !foundCron {
		t.Error("cron ticker not in Scheduled()")
	}
}

// Cancel removes a schedule, and a ticker that fires after cancellation does
// not re-arm itself.
func TestCancelStopsCron(t *testing.T) {
	e, _ := New(nil, nil)
	e.now = func() time.Time { return time.Date(2026, 7, 23, 9, 29, 59, 0, time.UTC) }
	var fires int
	Register(e, "job", func(context.Context, struct{}) error { fires++; return nil })
	id, _ := Every(e, "30 9 * * *", "job", struct{}{})

	tick := e.pending[0]
	if err := e.Cancel(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if len(e.Scheduled()) != 0 {
		t.Fatalf("after Cancel, %d still scheduled", len(e.Scheduled()))
	}

	// Simulate a tick that was already in flight when Cancel landed: running
	// it must neither fire the target nor re-arm.
	e.pending = nil
	if _, err := e.invoke(context.Background(), tick, e.flows[tick.Flow]); err != nil {
		t.Fatal(err)
	}
	if fires != 0 {
		t.Errorf("cancelled ticker fired %d times, want 0", fires)
	}
	if len(e.pending) != 0 {
		t.Errorf("cancelled ticker re-armed: %d pending", len(e.pending))
	}
}
