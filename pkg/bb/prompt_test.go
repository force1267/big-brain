package bb

import "testing"

func TestRender(t *testing.T) {
	p := Prompt("hi {user_message}, from {who}")
	got := p.Render(map[string]string{"user_message": "there", "who": "marvis"})
	if want := "hi there, from marvis"; got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
	// Unknown placeholder left as-is; no vars returns raw.
	if got := p.Render(nil); got != "hi {user_message}, from {who}" {
		t.Fatalf("Render(nil) = %q", got)
	}
	if got := Prompt("{a}").Render(map[string]string{"b": "x"}); got != "{a}" {
		t.Fatalf("unknown placeholder not preserved: %q", got)
	}
}
