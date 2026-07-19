package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeUpstream is a minimal OpenAI-compatible SSE endpoint.
func fakeUpstream(t *testing.T, status int, deltas ...string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string `json:"model"`
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("upstream decode: %v", err)
		}
		if status != http.StatusOK {
			http.Error(w, "nope", status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, d := range deltas {
			fmt.Fprintf(w, `data: {"object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":%q}}]}`+"\n\n", d)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func TestOpenAIStreamsDeltas(t *testing.T) {
	srv := fakeUpstream(t, http.StatusOK, "hel", "lo")
	defer srv.Close()

	m := OpenAI(srv.URL, "test-key", "gpt-test")
	stream, err := m.Stream(context.Background(),
		[]Message{{Role: "system", Content: "p"}, {Role: "user", Content: "hi"}, {Role: "assistant", Content: "prev"}},
		Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var got string
	for c := range stream {
		if c.Err != nil {
			t.Fatalf("chunk error: %v", c.Err)
		}
		got += c.Content
	}
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}

func TestOpenAIUpstreamFailure(t *testing.T) {
	srv := fakeUpstream(t, http.StatusInternalServerError)
	defer srv.Close()

	m := OpenAI(srv.URL, "test-key", "gpt-test")
	stream, err := m.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, Params{})
	if err == nil {
		// error may surface on first read instead of call
		for c := range stream {
			if c.Err != nil {
				err = c.Err
			}
		}
	}
	if !errors.Is(err, ErrUpstream) {
		t.Fatalf("err = %v; want ErrUpstream", err)
	}
}

func TestOpenAICancelledContext(t *testing.T) {
	srv := fakeUpstream(t, http.StatusOK, "a", "b", "c")
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	m := OpenAI(srv.URL, "test-key", "gpt-test")
	stream, err := m.Stream(ctx, []Message{{Role: "user", Content: "hi"}}, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	cancel()
	for range stream { // must terminate, not leak
	}
}

func TestCollect(t *testing.T) {
	stream, _ := (&Mock{Chunks: []string{"a", "b"}}).Stream(context.Background(), nil, Params{})
	if got, err := Collect(stream); err != nil || got != "ab" {
		t.Fatalf("got %q, %v", got, err)
	}
	boom := errors.New("boom")
	stream, _ = (&Mock{Chunks: []string{"a"}, Fail: boom}).Stream(context.Background(), nil, Params{})
	if got, err := Collect(stream); !errors.Is(err, boom) || got != "a" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestMockRecordsAndStreams(t *testing.T) {
	boom := errors.New("boom")
	m := &Mock{Chunks: []string{"a"}, Fail: boom}
	stream, err := m.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, Params{})
	if err != nil {
		t.Fatal(err)
	}
	var last Chunk
	var got string
	for c := range stream {
		last = c
		got += c.Content
	}
	if got != "a" || !errors.Is(last.Err, boom) || len(m.Got.Msgs) != 1 {
		t.Fatalf("got %q, last %+v, recorded %+v", got, last, m.Got.Msgs)
	}
}
