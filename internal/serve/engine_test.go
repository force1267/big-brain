package serve

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/pkg/engine"
)

// recordingStore wraps a Store and records every key Put, so a test can prove
// something actually wrote a checkpoint through it (not just that a store was
// passed in) without MemStore exposing its internal map.
type recordingStore struct {
	*engine.MemStore
	mu   sync.Mutex
	puts []string
}

func newRecordingStore() *recordingStore {
	return &recordingStore{MemStore: engine.NewMemStore()}
}

func (s *recordingStore) Put(ctx context.Context, key string, val []byte) error {
	s.mu.Lock()
	s.puts = append(s.puts, key)
	s.mu.Unlock()
	return s.MemStore.Put(ctx, key, val)
}

func (s *recordingStore) checkpointWrites() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range s.puts {
		if strings.HasPrefix(k, "flow/") {
			n++
		}
	}
	return n
}

// The engine scheduler registers a deferred body and the worker fires it: a
// once-job enqueued for now runs and the body executes.
func TestEngineSchedulerFires(t *testing.T) {
	sched, err := newEngineScheduler(engine.NewMemStore())
	if err != nil {
		t.Fatal(err)
	}
	fired := make(chan struct{})
	run := func(_ context.Context, _ []byte) error { close(fired); return nil }
	if err := sched.Defer("body", "", time.Now(), []byte(`{}`), run); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.run(ctx, 1)

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("deferred body did not fire")
	}
}

// End to end through build(): a registered Trigger→Once schedules a named body,
// and running the worker fires it.
func TestServeRunsTriggerChain(t *testing.T) {
	flow.ResetTriggers()
	t.Cleanup(flow.ResetTriggers)

	fired := make(chan struct{})
	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, chat *agent.ModelChat) error {
		close(fired)
		turn.Reply("done")
		return nil
	})).WithId("nightly")
	flow.Trigger().Next(flow.Once(time.Now())).Next(body)

	s, _, err := build(talkFlow("x"), Store(engine.NewMemStore()))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.sched.run(ctx, 1)

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("trigger chain body did not fire")
	}
}

// A Durable flow nested inside a triggered body actually checkpoints: the
// engine worker's ctx must carry a flow.Store keyed to the firing run, the
// same way a normal HTTP-served request's ctx does (next.md #2).
func TestTriggeredDurableFlowCheckpoints(t *testing.T) {
	store := newRecordingStore()
	sched, err := newEngineScheduler(store)
	if err != nil {
		t.Fatal(err)
	}

	fired := make(chan struct{})
	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		turn.Reply("done")
		return nil
	})).WithId("durable-body").Durable()

	run := func(rctx context.Context, _ []byte) error {
		defer close(fired)
		_, err := flow.Run(rctx, body, flow.State{}, nil)
		return err
	}
	if err := sched.Defer("job", "", time.Now(), []byte(`{}`), run); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.run(ctx, 1)

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("deferred body did not fire")
	}
	// give the checkpoint save (which runs after turn.Reply, inside the same
	// call) a moment to land — the flow.Run above has already returned by the
	// time fired closes, so this is just belt-and-suspenders against the race
	// detector's view of memory, not a real wait.
	if n := store.checkpointWrites(); n == 0 {
		t.Fatal("expected the Durable flow to write a checkpoint through the worker ctx's store, got none — worker ctx has no store wired")
	}
}

// The previous test only proves a checkpoint gets written; this one proves
// the checkpoint is actually honored on resume — a second firing that shares
// the same engine.RunID skips re-running the completed Durable step, same
// promise TestCheckpointResumes proves for the plain flow.WithCheckpoint
// path, now through the real triggered-engine wiring (next.md #2's stated
// gap in the original test).
//
// Defer always mints a fresh uuid per firing, so two Defer calls would each
// get their own checkpoint scope (correct for two distinct firings, but not
// a resume). EnqueueID lets the test pin the run id so both firings share
// one flow.WithStore(..., id) scope, simulating a resumed run of the same
// job (the same identity RunID's doc says survives a retry/resume).
func TestTriggeredDurableFlowResumeSkipsCompletedStep(t *testing.T) {
	store := engine.NewMemStore()
	sched, err := newEngineScheduler(store)
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int64
	fired := make(chan struct{}, 1)
	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		calls.Add(1)
		turn.Reply("done")
		return nil
	})).WithId("durable-resume-body").Durable()

	run := func(rctx context.Context, _ []byte) error {
		_, err := flow.Run(rctx, body, flow.State{}, nil)
		fired <- struct{}{}
		return err
	}
	// Register the wrapped handler for this bodyID (Defer's registration is
	// what wires flow.WithStore via engine.RunID); the far-future Once here
	// never fires during the test, it just triggers registration.
	if err := sched.Defer("durable-resume-job", "", time.Now().Add(time.Hour), []byte(`{}`), run); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.run(ctx, 1)

	const runID = "durable-resume-run"
	if _, err := sched.eng.EnqueueID(context.Background(), runID, "durable-resume-job", json.RawMessage(`{}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("first firing did not complete")
	}

	if _, err := sched.eng.EnqueueID(context.Background(), runID, "durable-resume-job", json.RawMessage(`{}`), time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("second (resumed) firing did not complete")
	}

	if calls.Load() != 1 {
		t.Fatalf("agent ran %d times across two same-run-id firings, want 1 (second should have resumed from checkpoint, not re-run)", calls.Load())
	}
}

// bb.Metadata[T] (next.md #7) is claimed to survive scheduling/replay "the
// same promise Payload already had" — proven for Payload by
// TestTriggeredDurableFlowCheckpoints/ResumeSkip's Store wiring, but the
// existing metadata tests (TestMetadataSeedAndReplay,
// TestWebhookPayloadAndSeedReplay) only round-trip the raw triggerPayload
// JSON directly, never through a real Durable() checkpoint/resume over the
// actual engineScheduler. This closes that gap: metadata seeded on a trigger
// must still be readable via turn.Metadata() on a resumed firing of the same
// run, after the Durable step it lives alongside was already checkpointed.
func TestTriggeredDurableFlowMetadataSurvivesResume(t *testing.T) {
	store := engine.NewMemStore()
	sched, err := newEngineScheduler(store)
	if err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int64
	var seenMeta []string
	fired := make(chan struct{}, 1)
	body := flow.New().WithAgent(agent.New().OnMessage(func(_ context.Context, turn *agent.Turn, _ *agent.ModelChat) error {
		calls.Add(1)
		seenMeta = append(seenMeta, string(turn.Metadata()))
		turn.Reply("done")
		return nil
	})).WithId("durable-meta-body").Durable()

	// Mirrors flow's unexported triggerPayload shape (internal/flow/trigger.go)
	// just enough to carry Meta through the same json.RawMessage the engine
	// treats as an opaque blob.
	type triggerPayload struct {
		Meta []byte `json:"meta,omitempty"`
	}

	const metaJSON = `{"X-Signature":"sig"}`
	run := func(rctx context.Context, raw []byte) error {
		var tp triggerPayload
		if err := json.Unmarshal(raw, &tp); err != nil {
			return err
		}
		rctx = agent.WithMetadata(rctx, tp.Meta)
		_, err := flow.Run(rctx, body, flow.State{}, nil)
		fired <- struct{}{}
		return err
	}
	if err := sched.Defer("durable-meta-job", "", time.Now().Add(time.Hour), []byte(`{}`), run); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.run(ctx, 1)

	payload, err := json.Marshal(triggerPayload{Meta: []byte(metaJSON)})
	if err != nil {
		t.Fatal(err)
	}
	const runID = "durable-meta-run"
	if _, err := sched.eng.EnqueueID(context.Background(), runID, "durable-meta-job", json.RawMessage(payload), time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("first firing did not complete")
	}

	if _, err := sched.eng.EnqueueID(context.Background(), runID, "durable-meta-job", json.RawMessage(payload), time.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("second (resumed) firing did not complete")
	}

	if calls.Load() != 1 {
		t.Fatalf("agent ran %d times across two same-run-id firings, want 1 (Durable step should have resumed, not re-run)", calls.Load())
	}
	if len(seenMeta) != 1 || seenMeta[0] != metaJSON {
		t.Fatalf("expected metadata %q seen once, got %v", metaJSON, seenMeta)
	}
}
