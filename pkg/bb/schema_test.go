package bb

import (
	"reflect"
	"testing"
)

func TestSchemaAndDecode(t *testing.T) {
	type intent struct {
		Intent string   `json:"intent"`
		Reason string   `json:"reason,omitempty"`
		Tags   []string `json:"tags"`
		hidden int      // unexported: ignored
	}
	s := Schema[intent]()

	got := s.JSONSchema()
	want := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"intent": map[string]any{"type": "string"},
			"reason": map[string]any{"type": "string"},
			"tags":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"intent", "tags"}, // reason is omitempty
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSONSchema mismatch:\n got %#v\nwant %#v", got, want)
	}

	iv, err := s.Decode([]byte(`{"intent":"talk","tags":["a","b"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if iv.Intent != "talk" || len(iv.Tags) != 2 {
		t.Fatalf("Decode = %+v", iv)
	}
}

func TestSchemaEnumAndDoc(t *testing.T) {
	type routed struct {
		Intent string `json:"intent" enum:"talk, house , remember" doc:"the chosen capability"`
	}
	props := Schema[routed]().JSONSchema()["properties"].(map[string]any)
	field := props["intent"].(map[string]any)
	enum, ok := field["enum"].([]any)
	if !ok || len(enum) != 3 || enum[0] != "talk" || enum[1] != "house" || enum[2] != "remember" {
		t.Fatalf("enum = %v (trimmed?)", field["enum"])
	}
	if field["description"] != "the chosen capability" {
		t.Fatalf("doc = %v", field["description"])
	}
}
