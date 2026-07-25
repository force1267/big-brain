package serve

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"

	"github.com/force1267/big-brain/internal/flow"
)

func flowN() flow.Flow { return flow.New().WithId("x") }

// Default precedence: named < unnamed < explicit-default < Serve-arg; within a
// rank, the last registration wins.
func TestRegistryPrecedence(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)

	// Only a named flow: it is the fallback default and reachable by name.
	nf := flowN()
	SetName(register(nf, "", rankNamed), "m/1") // simulate WithFlow().As
	named, def := resolveRegistry(nil)
	if named["m/1"] != nf || def != nf {
		t.Fatalf("named fallback default failed: def=%v named=%v", def, named)
	}

	// An unnamed flow outranks the named fallback.
	uf := flowN()
	AddUnnamed(uf)
	if _, def = resolveRegistry(nil); def != uf {
		t.Fatal("unnamed should outrank named")
	}

	// An explicit default outranks the unnamed.
	df := flowN()
	AddDefault(df)
	if _, def = resolveRegistry(nil); def != df {
		t.Fatal("WithDefaultFlow should outrank unnamed")
	}

	// A Serve-arg default outranks everything; named flow still routable.
	sf := flowN()
	named, def = resolveRegistry(sf)
	if def != sf || named["m/1"] != nf {
		t.Fatalf("serve-arg default / named routing failed: def=%v", def)
	}
}

// Last-within-rank wins: two unnamed flows, the later is the default.
func TestRegistryLastUnnamedWins(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)
	AddUnnamed(flowN())
	second := flowN()
	AddUnnamed(second)
	if _, def := resolveRegistry(nil); def != second {
		t.Fatal("last unnamed should win")
	}
}

// resolve routes by model id and falls back to the default for unknown ids.
func TestResolveRouting(t *testing.T) {
	a, b, d := flowN(), flowN(), flowN()
	s := &server{named: map[string]flow.Flow{"m/a": a, "m/b": b}, def: d, name: "def"}
	if f, n := s.resolve("m/a"); f != a || n != "m/a" {
		t.Fatalf("named route failed: %v %q", f, n)
	}
	if f, n := s.resolve("nope"); f != d || n != "def" {
		t.Fatalf("unknown should hit default: %v %q", f, n)
	}
	if f, n := s.resolve(""); f != d || n != "def" {
		t.Fatalf("empty should hit default: %v %q", f, n)
	}
}

// A duplicate flow name warns (last registration wins for that model id).
func TestDuplicateNameWarns(t *testing.T) {
	ResetRegistry()
	t.Cleanup(ResetRegistry)

	var buf bytes.Buffer
	old := logrus.StandardLogger().Out
	logrus.SetOutput(&buf)
	t.Cleanup(func() { logrus.SetOutput(old) })

	SetName(register(flowN(), "", rankNamed), "dup")
	last := flowN()
	SetName(register(last, "", rankNamed), "dup")

	if !strings.Contains(buf.String(), "registered more than once") {
		t.Fatalf("no duplicate warning: %q", buf.String())
	}
	if named, _ := resolveRegistry(nil); named["dup"] != last {
		t.Fatal("last duplicate registration should win")
	}
}
