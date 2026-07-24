package bb

import (
	"encoding/json"
	"reflect"
	"strings"
)

// Structured couples a Go type T to the JSON schema the model is asked to fill
// and the decode back into T. Build one with Schema[T]() — the type parameter
// is the whole point: no throwaway value to pass, and Decode hands you a typed
// T, not an any.
type Structured[T any] struct{}

// Schema returns the Structured descriptor for T, e.g. bb.Schema[intent]().
func Schema[T any]() Structured[T] { return Structured[T]{} }

// JSONSchema is the schema an Agent sends the model so its reply fits T. It is
// derived from T's fields and json tags by reflection.
func (Structured[T]) JSONSchema() map[string]any {
	return schemaOf(reflect.TypeFor[T]())
}

// Decode parses a model's JSON reply into T.
func (Structured[T]) Decode(data []byte) (T, error) {
	var v T
	err := json.Unmarshal(data, &v)
	return v, err
}

// Validate reports whether data decodes into T, without exposing T. It lets an
// agent that holds a schema check a model reply against it (the schema-mismatch
// error surfaces at Ask), matching the untyped Schema interface agents consume.
func (Structured[T]) Validate(data []byte) error {
	var v T
	return json.Unmarshal(data, &v)
}

// splitEnum turns an `enum:"a,b,c"` tag into a slice of allowed values, so a
// model is constrained to a fixed set for that field.
func splitEnum(tag string) []any {
	parts := strings.Split(tag, ",")
	out := make([]any, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// schemaOf builds a JSON Schema for a Go type: the common kinds (struct,
// string, bool, int, float, slice, map), with `doc:"…"` field descriptions and
// `enum:"a,b,c"` value constraints via struct tags. Formats and numeric bounds
// can be added the same way when a flow needs them.
func schemaOf(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		props := map[string]any{}
		var required []string
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name, omitempty, skip := jsonName(f)
			if skip {
				continue
			}
			s := schemaOf(f.Type)
			if doc := strings.TrimSpace(f.Tag.Get("doc")); doc != "" {
				s["description"] = doc
			}
			if enum := strings.TrimSpace(f.Tag.Get("enum")); enum != "" {
				s["enum"] = splitEnum(enum)
			}
			props[name] = s
			if !omitempty {
				required = append(required, name)
			}
		}
		out := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaOf(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object"}
	default:
		return map[string]any{} // unknown: accept anything
	}
}

// jsonName resolves a field's JSON name and whether it is omitempty. skip is
// true for json:"-".
func jsonName(f reflect.StructField) (name string, omitempty, skip bool) {
	tag := f.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	name = f.Name
	if tag != "" {
		parts := strings.Split(tag, ",")
		if parts[0] != "" {
			name = parts[0]
		}
		for _, p := range parts[1:] {
			if p == "omitempty" {
				omitempty = true
			}
		}
	}
	return name, omitempty, false
}
