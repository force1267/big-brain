package flow

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/force1267/big-brain/pkg/model"
)

// Store is the durability backend a flow checkpoints to — the two-method KV
// pkg/engine.Store already provides (satisfied structurally, so flow does not
// import engine). Serve installs one via bb.Store.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Put(ctx context.Context, key string, val []byte) error
}

// checkpoint memoizes each leaf flow's result per run, so a re-run (after a
// crash, when the client retries with the same run id) skips flows that already
// completed and resumes from the one that was interrupted. Leaf flows (Basic)
// are the expensive units — a model call — so those are what we persist;
// composite flows (seq/groups/select) are cheap structure that re-walks into
// cached children.
type checkpoint struct {
	store Store
	run   string
	stale bool // structure changed since the saved run: don't resume into it
}

type checkpointKey struct{}
type pathKey struct{}
type storeKey struct{}

// storeHandle is the durability backend made available for a run, but not yet
// active: only a Durable flow turns it into an actual checkpoint (opt-in). Serve
// installs it via WithStore; ambient checkpointing is gone.
type storeHandle struct {
	store Store
	run   string
}

// WithStore makes a store available to the run without checkpointing anything by
// itself. A Durable flow reads it (storeFrom) and activates a checkpoint for its
// subtree. This is the split that makes durability loud and opt-in.
func WithStore(ctx context.Context, store Store, run string) context.Context {
	if store == nil {
		return ctx
	}
	return context.WithValue(ctx, storeKey{}, storeHandle{store: store, run: run})
}

func storeFrom(ctx context.Context) (storeHandle, bool) {
	h, ok := ctx.Value(storeKey{}).(storeHandle)
	return h, ok
}

// activateDurable turns the available store into a live checkpoint for the
// subtree run under the returned context — called by a Durable flow. cfg governs
// the structure-version guard. If no store is available, ctx is unchanged.
func activateDurable(ctx context.Context, sig string, cfg durableConfig) context.Context {
	h, ok := storeFrom(ctx)
	if !ok {
		return ctx
	}
	cp := &checkpoint{store: h.store, run: h.run}
	if !cfg.forwardCompatible {
		cp.stale = cp.versionChanged(ctx, sig) // a changed graph → don't resume into it
	}
	return context.WithValue(ctx, checkpointKey{}, cp)
}

// WithCheckpoint installs a live checkpoint directly (used by tests and by
// activateDurable's older callers). Prefer WithStore + Durable.
func WithCheckpoint(ctx context.Context, store Store, run string) context.Context {
	if store == nil {
		return ctx
	}
	return context.WithValue(ctx, checkpointKey{}, &checkpoint{store: store, run: run})
}

func cpFrom(ctx context.Context) *checkpoint {
	cp, _ := ctx.Value(checkpointKey{}).(*checkpoint)
	return cp
}

// path identifies a flow by its position in the tree — deterministic across
// re-runs and independent of concurrent completion order, so it is a stable
// memo key. Composites extend it for their children; a leaf reads it.
func pathOf(ctx context.Context) string {
	p, _ := ctx.Value(pathKey{}).(string)
	return p
}

func withPath(ctx context.Context, seg string) context.Context {
	return context.WithValue(ctx, pathKey{}, pathOf(ctx)+"/"+seg)
}

func indexPath(ctx context.Context, i int) context.Context {
	return withPath(ctx, strconv.Itoa(i))
}

// versionChanged compares the durable flow's structure signature against the one
// saved for this run/path, recording the new one. It reports true when they
// differ (or none was saved), so a changed graph is not resumed into.
func (c *checkpoint) versionChanged(ctx context.Context, sig string) bool {
	key := "ver/" + c.run + pathOf(ctx)
	prev, ok, err := c.store.Get(ctx, key)
	if err == nil && (!ok || string(prev) != sig) {
		_ = c.store.Put(ctx, key, []byte(sig))
	}
	return err != nil || !ok || string(prev) != sig
}

// load returns a memoized State for the current path, if present. A stale
// checkpoint (structure changed) always misses, forcing a fresh run.
func (c *checkpoint) load(ctx context.Context) (State, bool) {
	if c.stale {
		return State{}, false
	}
	b, ok, err := c.store.Get(ctx, c.key(ctx))
	if err != nil || !ok {
		return State{}, false
	}
	var sn snapshot
	if json.Unmarshal(b, &sn) != nil {
		return State{}, false
	}
	return State{Chat: sn.Chat, selected: sn.Selected, hasSel: sn.HasSel}, true
}

// save memoizes a State for the current path.
func (c *checkpoint) save(ctx context.Context, s State) {
	b, err := json.Marshal(snapshot{Chat: s.Chat, Selected: s.selected, HasSel: s.hasSel})
	if err == nil {
		_ = c.store.Put(ctx, c.key(ctx), b)
	}
}

func (c *checkpoint) key(ctx context.Context) string {
	return "flow/" + c.run + pathOf(ctx)
}

// snapshot is the serializable form of State.
type snapshot struct {
	Chat     []model.Message `json:"chat"`
	Selected string          `json:"selected,omitempty"`
	HasSel   bool            `json:"has_sel,omitempty"`
}
