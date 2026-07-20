package brain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/force1267/big-brain/pkg/memory"
	"github.com/force1267/big-brain/pkg/model"
)

// ErrNoMemory is returned when a memory node runs without a Memory bound.
var ErrNoMemory = errors.New("no memory bound to run")

// RecallFacts returns a node that injects the brain's remembered facts as a
// system message, each tagged with when it was learned. notes are appended
// verbatim, one per line — use them for domain guidance on how the model
// should weigh the facts. If a fact's Content needs attribution (whose it
// is, what it's about), encode that in Content when you Remember it — the
// engine keeps no such concept. The query passed to Recall is the latest
// message's content; how many facts come back, and how (or whether) query
// is used to judge relevance, is entirely the bound Memory implementation's
// choice — RecallFacts takes no limit of its own.
func RecallFacts(notes ...string) Node {
	return Func(func(ctx context.Context, r *Run) error {
		if r.Memory == nil {
			return ErrNoMemory
		}
		facts, err := r.Memory.Recall(ctx, latestMessage(r.Messages))
		if err != nil {
			return err
		}
		if len(facts) == 0 {
			return nil
		}
		var b strings.Builder
		b.WriteString("Known facts, oldest first:\n")
		for _, f := range facts {
			fmt.Fprintf(&b, "- [%s] %s\n", f.At.Format("2006-01-02"), f.Content)
		}
		for _, n := range notes {
			b.WriteString(n + "\n")
		}
		r.Messages = append([]model.Message{{Role: "system", Content: b.String()}}, r.Messages...)
		return nil
	})
}

// latestMessage returns the content of the last message in msgs, or "" if
// there are none. Nodes prepend system messages, so the last entry is
// always the most recent actual turn regardless of pipeline shape.
func latestMessage(msgs []model.Message) string {
	if len(msgs) == 0 {
		return ""
	}
	return msgs[len(msgs)-1].Content
}

type memorized struct {
	Facts []string `json:"facts"`
}

// Memorize returns a node that decides — via the model bound to role,
// following instruction — whether the latest exchange contains facts worth
// keeping, and remembers each one. instruction must ask for a list of
// self-contained facts (any attribution — whose fact it is — belongs in
// the fact text itself, per instruction's wording); the model's answer is
// decoded into that list, nothing more. This is ambient memory: the
// pipeline decides, the caller never says "remember this". Place it after
// Reply; the pipeline keeps running once the reply has streamed.
func Memorize(role model.Role, instruction string) Node {
	extract := Extract[memorized](role, instruction, "_memorize")
	return Func(func(ctx context.Context, r *Run) error {
		if r.Memory == nil {
			return ErrNoMemory
		}
		if err := extract.Run(ctx, r); err != nil {
			return err
		}
		got, _ := Var[memorized](r, "_memorize")
		for _, content := range got.Facts {
			f := memory.Fact{Content: content, At: time.Now()}
			if err := r.Memory.Remember(ctx, f); err != nil {
				return err
			}
		}
		return nil
	})
}
