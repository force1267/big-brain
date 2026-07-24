package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// jsonRaw names the Flow payload type locally; the cron ticker ignores it.
type jsonRaw = json.RawMessage

// ErrCronSpec is returned when a crontab string cannot be parsed. Malformed
// schedules fail at Every (startup), never as a silent no-op.
var ErrCronSpec = errors.New("engine: invalid cron spec")

// tickPrefix marks a run id as a cron ticker (self-rescheduling). Cancel uses
// it to decide which cancellations need a re-arm-blocking tombstone.
const tickPrefix = "tick/"

// CronOpt configures a schedule.
type CronOpt func(*cronCfg)

type cronCfg struct{ catchup bool }

// Catchup makes a schedule fire the target once for every tick that elapsed
// while the process was down, instead of the default single late fire. Use it
// when missing a run matters (e.g. per-interval accounting); leave it off when
// only the current state matters (e.g. a periodic poll).
func Catchup() CronOpt { return func(c *cronCfg) { c.catchup = true } }

// Every schedules flow to run on a standard 5-field crontab spec
// ("min hour dom mon dow"), with payload as its input. The schedule is
// durable: it rides the engine's own queue as a self-rescheduling run, so it
// survives restarts and needs no separate timer loop. By default a tick missed
// while the process was down fires once late; pass Catchup() to fire every
// missed tick. Every returns the schedule's handle ID (the same ID Scheduled
// reports), which Cancel takes to stop the schedule.
func Every(e *Engine, spec, flow string, payload any, opts ...CronOpt) (string, error) {
	sc, err := parseCron(spec)
	if err != nil {
		return "", err
	}
	var cfg cronCfg
	for _, o := range opts {
		o(&cfg)
	}
	tickFlow := "cron:" + spec + ":" + flow
	id := tickID(tickFlow)
	// The ticker flow: fire the target (for the due tick, plus every missed tick
	// when catch-up is on), then re-arm itself for the next tick — unless it has
	// been cancelled, in which case it does neither. Its payload is this tick's
	// scheduled time, so it can tell how many ticks elapsed.
	if err := e.Register(tickFlow, func(ctx context.Context, raw jsonRaw) error {
		if e.cancelled(ctx, id) {
			return nil
		}
		if err := e.fire(ctx, flow, payload, sc, cfg, raw); err != nil {
			return err
		}
		next := sc.next(e.now())
		_, err := e.EnqueueID(ctx, id, tickFlow, next, next)
		return err
	}); err != nil {
		return "", err
	}

	// Arm the first tick (idempotent: a reload already carrying it is a no-op).
	first := sc.next(e.now())
	return e.EnqueueID(context.Background(), id, tickFlow, first, first)
}

// fire enqueues the target once, or — with catch-up — once per scheduled tick
// from the payload's scheduled time up to now.
func (e *Engine) fire(ctx context.Context, flow string, payload any, sc schedule, cfg cronCfg, raw jsonRaw) error {
	if cfg.catchup {
		if scheduled, ok := decodeTime(raw); ok {
			for t := scheduled; !t.After(e.now()); t = sc.next(t) {
				if _, err := e.Enqueue(ctx, flow, payload, time.Time{}); err != nil {
					return err
				}
			}
			return nil
		}
	}
	_, err := e.Enqueue(ctx, flow, payload, time.Time{})
	return err
}

func decodeTime(raw jsonRaw) (time.Time, bool) {
	var t time.Time
	if len(raw) == 0 || json.Unmarshal(raw, &t) != nil {
		return time.Time{}, false
	}
	return t, true
}

func tickID(tickFlow string) string { return tickPrefix + tickFlow }

// cronSpec extracts the crontab spec from a ticker flow name, or reports false
// for a non-cron flow. Ticker names are "cron:<spec>:<flow>", and specs never
// contain a colon, so the last colon separates spec from target flow.
func cronSpec(flow string) (string, bool) {
	rest, ok := strings.CutPrefix(flow, "cron:")
	if !ok {
		return "", false
	}
	if i := strings.LastIndexByte(rest, ':'); i >= 0 {
		return rest[:i], true
	}
	return "", false
}

// schedule is a parsed crontab spec: the set of allowed values per field.
type schedule struct {
	min, hour, dom, mon, dow uint64 // bitmasks
}

func parseCron(spec string) (schedule, error) {
	f := strings.Fields(spec)
	if len(f) != 5 {
		return schedule{}, fmt.Errorf("%w: want 5 fields, got %d", ErrCronSpec, len(f))
	}
	var s schedule
	var err error
	if s.min, err = field(f[0], 0, 59); err != nil {
		return s, err
	}
	if s.hour, err = field(f[1], 0, 23); err != nil {
		return s, err
	}
	if s.dom, err = field(f[2], 1, 31); err != nil {
		return s, err
	}
	if s.mon, err = field(f[3], 1, 12); err != nil {
		return s, err
	}
	if s.dow, err = field(f[4], 0, 6); err != nil {
		return s, err
	}
	return s, nil
}

// field parses one crontab field into a bitmask of allowed values. Supports
// "*", "a", "a-b", "a,b", "*/n", and "a-b/n".
func field(tok string, min, max int) (uint64, error) {
	var mask uint64
	for _, part := range strings.Split(tok, ",") {
		step := 1
		rng := part
		if i := strings.IndexByte(part, '/'); i >= 0 {
			rng = part[:i]
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n < 1 {
				return 0, fmt.Errorf("%w: step %q", ErrCronSpec, part)
			}
			step = n
		}
		lo, hi := min, max
		if rng != "*" {
			if i := strings.IndexByte(rng, '-'); i >= 0 {
				a, err1 := strconv.Atoi(rng[:i])
				b, err2 := strconv.Atoi(rng[i+1:])
				if err1 != nil || err2 != nil {
					return 0, fmt.Errorf("%w: range %q", ErrCronSpec, part)
				}
				lo, hi = a, b
			} else {
				n, err := strconv.Atoi(rng)
				if err != nil {
					return 0, fmt.Errorf("%w: value %q", ErrCronSpec, part)
				}
				lo, hi = n, n
			}
		}
		if lo < min || hi > max || lo > hi {
			return 0, fmt.Errorf("%w: %q out of [%d,%d]", ErrCronSpec, part, min, max)
		}
		for v := lo; v <= hi; v += step {
			mask |= 1 << uint(v)
		}
	}
	return mask, nil
}

func (s schedule) matches(t time.Time) bool {
	return s.min&(1<<uint(t.Minute())) != 0 &&
		s.hour&(1<<uint(t.Hour())) != 0 &&
		s.dom&(1<<uint(t.Day())) != 0 &&
		s.mon&(1<<uint(t.Month())) != 0 &&
		s.dow&(1<<uint(t.Weekday())) != 0
}

// next returns the first minute strictly after t that matches the schedule.
// It scans minute by minute up to a year out, then gives up (a schedule that
// never fires within a year is treated as unreachable).
func (s schedule) next(t time.Time) time.Time {
	t = t.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(1, 0, 0)
	for ; t.Before(limit); t = t.Add(time.Minute) {
		if s.matches(t) {
			return t
		}
	}
	return limit // unreachable-in-a-year: park it a year out
}
