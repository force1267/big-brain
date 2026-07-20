package memory

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/force1267/big-brain/pkg/model"
)

// ErrRecall wraps failures judging relevance during Recall.
var ErrRecall = errors.New("recall failed")

// OpenLLM returns a Memory backed by the same append-only JSONL log as
// OpenFile, but Recall works like a one-call semantic search: every stored
// fact plus query is handed to m in a single prompt, and m decides which
// facts are relevant — instead of returning facts by recency. This is a
// second Memory implementation exercising the interface's real contract:
// relevance is the implementation's choice, not a fixed policy (see
// memory.go). Facts remain durable the same way OpenFile's are.
func OpenLLM(path string, m model.Model, limit int) (Memory, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	var facts []Fact
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var fact Fact
		if err := json.Unmarshal(sc.Bytes(), &fact); err != nil {
			return nil, fmt.Errorf("%w: corrupt line: %w", ErrOpen, err)
		}
		facts = append(facts, fact)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrOpen, err)
	}
	return Monitored(&llmMemory{file: f, facts: facts, model: m, limit: limit}), nil
}

type llmMemory struct {
	mu    sync.Mutex
	file  *os.File
	facts []Fact
	model model.Model
	limit int
}

var _ Memory = (*llmMemory)(nil)

func (m *llmMemory) Remember(_ context.Context, f Fact) error {
	line, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrPersist, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.file.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("%w: %w", ErrPersist, err)
	}
	if err := m.file.Sync(); err != nil {
		return fmt.Errorf("%w: %w", ErrPersist, err)
	}
	m.facts = append(m.facts, f)
	return nil
}

// Recall asks m, in one call, which stored facts are relevant to query.
// With no query or no facts yet, it falls back to the most-recent-N
// behavior OpenFile uses — there's nothing to judge relevance against.
func (m *llmMemory) Recall(ctx context.Context, query string) ([]Fact, error) {
	m.mu.Lock()
	facts := make([]Fact, len(m.facts))
	copy(facts, m.facts)
	m.mu.Unlock()

	if len(facts) == 0 || query == "" {
		return capFacts(facts, m.limit), nil
	}

	prompt := buildRecallPrompt(facts, query, m.limit)
	stream, err := m.model.Stream(ctx, []model.Message{{Role: "system", Content: prompt}}, model.Params{})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRecall, err)
	}
	text, err := model.Collect(stream)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRecall, err)
	}
	var idx []int
	if err := strictJSON(text, &idx); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRecall, err)
	}

	var out []Fact
	for _, i := range idx {
		if i < 0 || i >= len(facts) {
			continue
		}
		out = append(out, facts[i])
		if m.limit > 0 && len(out) >= m.limit {
			break
		}
	}
	return out, nil
}

func buildRecallPrompt(facts []Fact, query string, limit int) string {
	var b strings.Builder
	b.WriteString("Facts, numbered:\n")
	for i, f := range facts {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i, f.At.Format("2006-01-02"), f.Content)
	}
	fmt.Fprintf(&b, "\nQuery: %s\n\n", query)
	want := "as many as are relevant, most relevant first"
	if limit > 0 {
		want = fmt.Sprintf("at most %d, most relevant first", limit)
	}
	fmt.Fprintf(&b, "Return %s of the fact numbers above that are relevant to the query. "+
		"Respond with only a JSON array of integers, e.g. [2,0]. Empty array if none are relevant.", want)
	return b.String()
}

func capFacts(facts []Fact, limit int) []Fact {
	if limit > 0 && len(facts) > limit {
		facts = facts[len(facts)-limit:]
	}
	out := make([]Fact, len(facts))
	copy(out, facts)
	return out
}

// strictJSON decodes text into out, tolerating prose/fences around the
// JSON value — small models like to wrap output despite instructions, the
// same problem pkg/brain.Extract solves for object output.
func strictJSON(text string, out any) error {
	start := strings.IndexAny(text, "[{")
	if start < 0 {
		return fmt.Errorf("no JSON value in %q", text)
	}
	return json.NewDecoder(bytes.NewReader([]byte(text[start:]))).Decode(out)
}
