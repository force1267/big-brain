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
}

type checkpointKey struct{}
type pathKey struct{}

// WithCheckpoint installs durable checkpointing for a run: results are keyed by
// (run, structural path) in store. Without it, flows always execute (no
// persistence). It is what bb.Store enables on Serve.
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

// load returns a memoized State for the current path, if present.
func (c *checkpoint) load(ctx context.Context) (State, bool) {
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
