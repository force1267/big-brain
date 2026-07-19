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

// RecallFacts returns a node that injects the brain's remembered facts as
// a system message, tagged with whose fact each is and who is speaking
// now. The model judges relevance itself.
func RecallFacts(limit int) Node {
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
				who = "household"
			}
			fmt.Fprintf(&b, "- [%s, %s] %s\n", who, f.At.Format("2006-01-02"), f.Content)
		}
		b.WriteString("Each fact is tagged [whose, when]. Facts tagged with a name belong to that person only; prefer the current speaker's and the household's facts.\n")
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

const memorizeInstruction = `Does the user's latest message state durable
facts worth remembering long-term (preferences, appointments, dates,
relationships, standing household rules)? List them, each self-contained,
in third person, saying "the speaker" for the person talking (never "the
user"). Leave the list empty for small talk, questions, or one-off
requests.`

// Memorize returns a node that decides — via the model bound to role —
// whether the latest exchange contains facts worth keeping, and remembers
// them for the current speaker. This is ambient memory: the pipeline
// decides, the user never says "remember this". Place it after Reply; the
// pipeline keeps running once the reply has streamed.
func Memorize(role model.Role) Node {
	extract := Extract[memorized](role, memorizeInstruction, "_memorize")
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
