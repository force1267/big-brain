package model

import "testing"

func TestJoinAssistantText(t *testing.T) {
	cases := []struct {
		name string
		msgs []Message
		want string
	}{
		{"empty", nil, ""},
		{
			"single assistant message",
			[]Message{{Role: "assistant", Content: "hi"}},
			"hi",
		},
		{
			"joins multiple with a blank line",
			[]Message{{Role: "assistant", Content: "one"}, {Role: "assistant", Content: "two"}},
			"one\n\ntwo",
		},
		{
			"skips non-assistant roles",
			[]Message{{Role: "user", Content: "ignored"}, {Role: "assistant", Content: "kept"}, {Role: "tool", Content: "ignored"}},
			"kept",
		},
		{
			"skips empty-content assistant messages (e.g. a bare tool-call carrier)",
			[]Message{{Role: "assistant", Content: ""}, {Role: "assistant", Content: "kept"}},
			"kept",
		},
		{
			"all skipped yields empty, not a stray separator",
			[]Message{{Role: "user", Content: "x"}, {Role: "assistant", Content: ""}},
			"",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := JoinAssistantText(c.msgs); got != c.want {
				t.Fatalf("JoinAssistantText(%+v) = %q, want %q", c.msgs, got, c.want)
			}
		})
	}
}
