package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/force1267/big-brain/pkg/model"
)

// ErrExtract wraps structured-output failures after repair was attempted.
var ErrExtract = errors.New("structured output failed")

// Extract returns a node that asks the model bound to role for a JSON
// object matching T, following instruction, and stores the decoded T under
// key in the run's Vars. Validation comes first: the output must strictly
// unmarshal into T (unknown fields rejected); only on mismatch is one
// repair round-trip made, carrying the decode error back to the model.
func Extract[T any](role model.Role, instruction, key string) Node {
	return Func(func(ctx context.Context, r *Run) error {
		m, ok := r.Models[role]
		if !ok {
			return fmt.Errorf("%w: %q", ErrNoModel, role)
		}

		var shape T
		shapeJSON, err := json.Marshal(shape)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrExtract, err)
		}
		msgs := append(append([]model.Message{}, r.Messages...), model.Message{
			Role: "system",
			Content: instruction + "\nRespond with only a JSON object of this exact shape, no prose, no code fences: " +
				string(shapeJSON),
		})

		// Extraction is machinery, not the caller's completion: caller
		// sampling params are deliberately not passed.
		text, err := ask(ctx, m, msgs)
		if err != nil {
			return err
		}
		var out T
		if decodeErr := strict(text, &out); decodeErr != nil {
			msgs = append(msgs,
				model.Message{Role: "assistant", Content: text},
				model.Message{Role: "system", Content: "That was not valid: " + decodeErr.Error() +
					". Respond again with only the corrected JSON object."})
			text, err = ask(ctx, m, msgs)
			if err != nil {
				return err
			}
			if decodeErr = strict(text, &out); decodeErr != nil {
				return fmt.Errorf("%w: after repair: %w", ErrExtract, decodeErr)
			}
		}
		r.SetVar(key, out)
		return nil
	})
}

func ask(ctx context.Context, m model.Model, msgs []model.Message) (string, error) {
	stream, err := m.Stream(ctx, msgs, model.Params{})
	if err != nil {
		return "", err
	}
	return model.Collect(stream)
}

// strict decodes text into out, rejecting unknown fields. Small models like
// to wrap JSON in prose or fences despite instructions, so decoding starts
// at the first brace and stops at the matching end of the object.
func strict(text string, out any) error {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return fmt.Errorf("no JSON object in %q", text)
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(text[start:])))
	dec.DisallowUnknownFields()
	return dec.Decode(out)
}
