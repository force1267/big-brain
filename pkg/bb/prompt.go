package bb

import "strings"

// Template is a customizable piece of text with {name} placeholders, built by
// Prompt. Render fills the placeholders; unknown ones are left untouched so a
// partially-filled template is still legible.
type Template struct{ raw string }

// Prompt makes a Template from text containing {name} placeholders, e.g.
// bb.Prompt("the user said:\n{user_message}\n").
func Prompt(raw string) Template { return Template{raw: raw} }

// Render substitutes {key} with vars[key] for each entry. Keys absent from
// vars are left in place.
func (t Template) Render(vars map[string]string) string {
	if len(vars) == 0 {
		return t.raw
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(t.raw)
}

// String returns the unrendered template text.
func (t Template) String() string { return t.raw }
