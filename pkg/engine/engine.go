package engine

import (
	"container/heap"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// runHeap is a min-heap of pending runs ordered by Wake, so the dispatcher
// finds the earliest-due run in O(log n) instead of an O(n) sorted insert.
type runHeap []Run

func (h runHeap) Len() int            { return len(h) }
func (h runHeap) Less(i, j int) bool  { return h[i].Wake.Before(h[j].Wake) }
func (h runHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *runHeap) Push(x any)         { *h = append(*h, x.(Run)) }
func (h *runHeap) Pop() any {
	old := *h
	n := len(old)
	r := old[n-1]
	*h = old[:n-1]
	return r
}

// Run is one durable invocation of a flow: the unit the engine leases,
// executes, and acknowledges. It is persisted until it completes, so a crash
// mid-run leaves the record behind to be resumed (at-least-once).
type Run struct {
	ID      string          `json:"id"`
	Flow    string          `json:"flow"`
	Input   json.RawMessage `json:"input"`
	Wake    time.Time       `json:"wake"`    // not eligible to run before this
	Attempt int             `json:"attempt"` // whole-run attempts (crash/yield resumes)
}

// Flow is an author's function: plain Go, using Step/Sleep for durability.
// Input is the enqueued payload; decode it, or register with the typed
// Register helper.
type Flow func(ctx context.Context, in json.RawMessage) error

// Engine runs flows durably. The zero-config path — New(nil, nil) — is an
// in-memory, untraced, single-process engine. Swap Store/Tracer for
// persistence and observability without touching flow code.
type Engine struct {
	store Store
	tr    Tracer
	now   func() time.Time

	mu      sync.Mutex
	flows   map[string]Flow
	pending runHeap       // scheduled runs, min-heap by Wake; source of truth in store
	wake    chan struct{} // nudges the dispatcher when pending changes
}

const indexKey = "runs/index" // holds []string of pending run IDs

// New returns an Engine. A nil store uses MemStore; a nil tracer uses NoTrace.
// It loads any pending runs a persistent store carries, so restarting the
// process resumes in-flight work.
func New(store Store, tr Tracer) (*Engine, error) {
	if store == nil {
		store = NewMemStore()
	}
	if tr == nil {
		tr = NoTrace{}
	}
	e := &Engine{
		store: store,
		tr:    tr,
		now:   time.Now,
		flows: map[string]Flow{},
		wake:  make(chan struct{}, 1),
	}
	if err := e.load(); err != nil {
		return nil, err
	}
	return e, nil
}

// Register binds a name to a flow. Enqueue and persisted runs reference it by
// name; a name is the one string that must stay stable across refactors.
func (e *Engine) Register(name string, f Flow) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, dup := e.flows[name]; dup {
		return fmt.Errorf("%w: %q", ErrDupFlow, name)
	}
	e.flows[name] = f
	return nil
}

// Register binds a name to a typed flow, decoding the JSON payload into P.
// It is a free function because Go methods cannot take type parameters.
func Register[P any](e *Engine, name string, fn func(context.Context, P) error) error {
	return e.Register(name, func(ctx context.Context, in json.RawMessage) error {
		var p P
		if len(in) > 0 {
			if err := json.Unmarshal(in, &p); err != nil {
				return fmt.Errorf("engine: decode input for %q: %w", name, err)
			}
		}
		return fn(ctx, p)
	})
}

// Enqueue schedules a run of the named flow with payload, to become eligible
// at-or-after when (zero = now). It persists before returning, so the run
// survives a crash between Enqueue and execution.
func (e *Engine) Enqueue(ctx context.Context, flow string, payload any, when time.Time) (string, error) {
	return e.EnqueueID(ctx, uuid.NewString(), flow, payload, when)
}

// EnqueueID is Enqueue with a caller-chosen run ID. If a run with that ID is
// already pending, it is left untouched and the call is a no-op — this makes
// bootstrapping a singleton run (a cron ticker) idempotent across restarts.
func (e *Engine) EnqueueID(ctx context.Context, id, flow string, payload any, when time.Time) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	for _, p := range e.pending {
		if p.ID == id {
			e.mu.Unlock()
			return id, nil // already scheduled; don't duplicate
		}
	}
	e.mu.Unlock()

	r := Run{ID: id, Flow: flow, Input: b, Wake: when}
	if err := e.persist(ctx, r); err != nil {
		return "", err
	}
	e.mu.Lock()
	e.insert(r)
	e.mu.Unlock()
	e.nudge()
	return id, nil
}

// Schedule is a pending run as seen by Scheduled: a cancellable handle plus
// enough to describe it to a human or a model. A non-empty Cron marks a
// recurring ticker and carries its crontab spec.
type Schedule struct {
	ID   string    `json:"id"`
	Flow string    `json:"flow"`
	Wake time.Time `json:"wake"`
	Cron string    `json:"cron,omitempty"`
}

// Scheduled returns a snapshot of every pending run — one-shot deferrals and
// cron tickers alike. A brain hands this list to a model (or its own logic) to
// decide what to Cancel.
func (e *Engine) Scheduled() []Schedule {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Schedule, 0, len(e.pending))
	for _, p := range e.pending {
		s := Schedule{ID: p.ID, Flow: p.Flow, Wake: p.Wake}
		if spec, ok := cronSpec(p.Flow); ok {
			s.Cron = spec
		}
		out = append(out, s)
	}
	return out
}

// Cancel drops a pending run by ID: a one-shot deferral or a cron ticker. It
// tombstones the ID so a ticker already mid-fire will not re-arm itself, and
// traces the cancellation for audit. Cancelling an unknown or already-run ID
// is a no-op (nil error) — the desired state (not scheduled) already holds.
//
// The cancelled/<id> tombstone is written only for cron tickers — the only runs
// that re-arm themselves and so can race a Cancel. Ticker ids are a finite set
// (one per cron spec, reused across re-arms), so tombstones are bounded. One-off
// runs never re-arm, so cancelling one needs no tombstone at all — which is what
// keeps a brain that cancels at high volume from accumulating them.
func (e *Engine) Cancel(ctx context.Context, id string) error {
	e.mu.Lock()
	kept := e.pending[:0]
	for _, p := range e.pending {
		if p.ID != id {
			kept = append(kept, p)
		}
	}
	e.pending = kept
	heap.Init(&e.pending)
	e.mu.Unlock()

	if strings.HasPrefix(id, tickPrefix) {
		if err := e.store.Put(ctx, "cancelled/"+id, []byte("1")); err != nil {
			return err
		}
	}
	e.ack(ctx, id)
	e.nudge() // dispatcher may be timing the run we just removed
	e.tr.Trace(ctx, StepRecord{Run: id, Step: "<cancel>"})
	return nil
}

// cancelled reports whether id has been tombstoned by Cancel.
func (e *Engine) cancelled(ctx context.Context, id string) bool {
	_, ok, _ := e.store.Get(ctx, "cancelled/"+id)
	return ok
}

// Run executes flows until ctx is cancelled, with n concurrent workers
// (n<=0 means 1). It blocks; run it in a goroutine if serving alongside HTTP.
func (e *Engine) Run(ctx context.Context, n int) error {
	if n <= 0 {
		n = 1
	}
	ready := make(chan Run)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			for r := range ready {
				e.exec(ctx, r)
			}
		}()
	}
	e.dispatch(ctx, ready) // returns on ctx.Done
	close(ready)
	wg.Wait()
	return ctx.Err()
}

// dispatch hands due runs to workers, sleeping until the earliest run is due
// or new work arrives.
func (e *Engine) dispatch(ctx context.Context, ready chan<- Run) {
	for {
		e.mu.Lock()
		var (
			next  Run
			has   bool
			delay time.Duration
		)
		if len(e.pending) > 0 {
			next = e.pending[0]
			has = true
			delay = next.Wake.Sub(e.now())
		}
		e.mu.Unlock()

		switch {
		case !has:
			select {
			case <-ctx.Done():
				return
			case <-e.wake:
			}
		case delay > 0:
			t := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-e.wake:
				t.Stop()
			case <-t.C:
			}
		default:
			// due: claim the earliest run (pop from the heap; still in store
			// until acked) and hand it to a worker.
			e.mu.Lock()
			if len(e.pending) == 0 || e.pending[0].ID != next.ID {
				e.mu.Unlock()
				continue // changed under us; re-evaluate
			}
			claimed := heap.Pop(&e.pending).(Run)
			e.mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case ready <- claimed:
			}
		}
	}
}

// exec runs one flow invocation and settles it: acknowledge on completion,
// requeue on a durable Sleep yield.
func (e *Engine) exec(ctx context.Context, r Run) {
	e.mu.Lock()
	f, ok := e.flows[r.Flow]
	e.mu.Unlock()
	if !ok {
		// Unknown flow (e.g. reloaded by a binary missing it). Leave the run
		// persisted and drop it from this process's pending set — loudly.
		e.tr.Trace(ctx, StepRecord{Run: r.ID, Flow: r.Flow, Step: "<dispatch>", Err: ErrUnknownFlow.Error()})
		return
	}

	requeue, err := e.invoke(ctx, r, f)
	if requeue != nil {
		r.Wake = *requeue
		r.Attempt++
		if perr := e.persist(ctx, r); perr != nil {
			return
		}
		e.mu.Lock()
		e.insert(r)
		e.mu.Unlock()
		e.nudge()
		return
	}
	// Completed (success or terminal failure): ack by deleting the record.
	if err != nil {
		e.tr.Trace(ctx, StepRecord{Run: r.ID, Flow: r.Flow, Step: "<flow>", Err: err.Error()})
	}
	e.ack(ctx, r.ID)
}

// invoke calls the flow, translating a Sleep yield (panic) into a requeue time.
func (e *Engine) invoke(ctx context.Context, r Run, f Flow) (requeue *time.Time, err error) {
	rc := &rt{run: r, store: e.store, tr: e.tr, now: e.now, seen: map[string]bool{}}
	defer func() {
		if p := recover(); p != nil {
			if y, ok := p.(yield); ok {
				w := y.wake
				requeue, err = &w, nil
				return
			}
			panic(p) // a real panic in flow code: let it surface
		}
	}()
	err = f(withRT(ctx, rc), r.Input)
	return nil, err
}

// --- persistence of the pending set over Store ---

func (e *Engine) persist(ctx context.Context, r Run) error {
	if err := putJSON(ctx, e.store, "run/"+r.ID, r); err != nil {
		return err
	}
	return e.addIndex(ctx, r.ID)
}

func (e *Engine) ack(ctx context.Context, id string) {
	// Best-effort: a failed delete only means the run may re-run, which the
	// at-least-once contract already permits.
	ids, _, _ := getJSON[[]string](ctx, e.store, indexKey)
	out := ids[:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	putJSON(ctx, e.store, indexKey, out)
	e.store.Put(ctx, "run/"+id, []byte("null"))
}

func (e *Engine) addIndex(ctx context.Context, id string) error {
	ids, _, err := getJSON[[]string](ctx, e.store, indexKey)
	if err != nil {
		return err
	}
	if slices.Contains(ids, id) {
		return nil
	}
	return putJSON(ctx, e.store, indexKey, append(ids, id))
}

func (e *Engine) load() error {
	ctx := context.Background()
	ids, _, err := getJSON[[]string](ctx, e.store, indexKey)
	if err != nil {
		return err
	}
	for _, id := range ids {
		r, ok, err := getJSON[Run](ctx, e.store, "run/"+id)
		if err != nil || !ok || r.ID == "" {
			continue // acked-but-index-stale, or corrupt; skip
		}
		e.insert(r)
	}
	return nil
}

// insert adds a run to the pending min-heap (O(log n)). Caller holds e.mu.
func (e *Engine) insert(r Run) {
	heap.Push(&e.pending, r)
}

func (e *Engine) nudge() {
	select {
	case e.wake <- struct{}{}:
	default:
	}
}
