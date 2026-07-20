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
// system message, tagged with whose fact each is (or "shared" if no
// speaker) and when, plus who is currently speaking. notes are appended
// verbatim, one per line — use them for domain guidance on how the model
// should weigh the facts (whose take priority, how to handle conflicts).
// The model judges relevance itself.
func RecallFacts(limit int, notes ...string) Node {
	return Func(func(ctx context.Context, r *Run) error {
		if r.Memory == nil {
			return ErrNoMemory
		}
		facts, err := r.Memory.Recall(ctx, limit)
		if err != nil {
			return err
		}
		if len(facts) == 0 {
			return nil
		}
		var b strings.Builder
		b.WriteString("Known facts, oldest first:\n")
		for _, f := range facts {
			who := f.Speaker
			if who == "" {
				who = "shared"
			}
			fmt.Fprintf(&b, "- [%s, %s] %s\n", who, f.At.Format("2006-01-02"), f.Content)
		}
		b.WriteString("Each fact is tagged [whose, when].\n")
		for _, n := range notes {
			b.WriteString(n + "\n")
		}
		if r.Speaker != "" {
			fmt.Fprintf(&b, "The current speaker is %s.", r.Speaker)
		}
		r.Messages = append([]model.Message{{Role: "system", Content: b.String()}}, r.Messages...)
		return nil
	})
}

type memorized struct {
	Facts []string `json:"facts"`
}

// Memorize returns a node that decides — via the model bound to role,
// following instruction — whether the latest exchange contains facts worth
// keeping, and remembers each one for the current speaker. instruction
// must ask for a list of self-contained facts; the model's answer is
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
			f := memory.Fact{Speaker: r.Speaker, Content: content, At: time.Now()}
			if err := r.Memory.Remember(ctx, f); err != nil {
				return err
			}
		}
		return nil
	})
}
