package serve

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/force1267/big-brain/internal/agent"
	"github.com/force1267/big-brain/internal/flow"
	"github.com/force1267/big-brain/pkg/model"
)

func setupReader(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(noop.NewMeterProvider()) })
	return reader
}

func findMetric(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			if met.Name == name {
				return met, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

// request.seconds is recorded for both streaming and non-streaming requests,
// tagged with the resolved flow name, outcome, and the streaming flag.
func TestServeRequestSecondsRecorded(t *testing.T) {
	reader := setupReader(t)
	s := serverFor(talkFlow("hi"))

	s.openai(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)))
	s.openai(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"hi"}]}`)))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	met, ok := findMetric(rm, "request.seconds")
	if !ok {
		t.Fatalf("request.seconds not recorded")
	}
	hist := met.Data.(metricdata.Histogram[float64])
	if len(hist.DataPoints) != 2 {
		t.Fatalf("want 2 data points (streaming + non-streaming), got %d", len(hist.DataPoints))
	}
	sawStreaming, sawBuffered := false, false
	for _, dp := range hist.DataPoints {
		flow, _ := dp.Attributes.Value(attribute.Key("flow"))
		outcome, _ := dp.Attributes.Value(attribute.Key("outcome"))
		if flow.AsString() != "brain" || outcome.AsString() != "ok" {
			t.Fatalf("unexpected attributes: %+v", dp.Attributes)
		}
		if streaming, _ := dp.Attributes.Value(attribute.Key("streaming")); streaming.AsBool() {
			sawStreaming = true
		} else {
			sawBuffered = true
		}
	}
	if !sawStreaming || !sawBuffered {
		t.Fatalf("want both streaming and buffered data points, got streaming=%v buffered=%v", sawStreaming, sawBuffered)
	}
}

// request.ttft.seconds is recorded only for a streaming request whose sink was
// actually written to — a non-streaming request records none at all.
func TestServeRequestTTFTOnlyOnStreaming(t *testing.T) {
	reader := setupReader(t)
	s := serverFor(talkFlow("hi"))

	s.openai(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)))

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if _, ok := findMetric(rm, "request.ttft.seconds"); ok {
		t.Fatalf("non-streaming request must not record request.ttft.seconds")
	}

	s.openai(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if _, ok := findMetric(rm, "request.ttft.seconds"); !ok {
		t.Fatalf("streaming request should have recorded request.ttft.seconds")
	}
}

// The client's own usage block reports the sum of every upstream model call
// the run made, in both non-streaming responses.
func TestServeNonStreamingResponsesCarryUsage(t *testing.T) {
	usageFlow := func(u model.Usage, reply string) flow.Flow {
		return flow.New().WithAgent(agent.New().WithModel(model.Bound(&model.Mock{Chunks: []string{reply}, Usage: &u}))).WithId("talk")
	}

	t.Run("openai", func(t *testing.T) {
		s := serverFor(usageFlow(model.Usage{Input: 7, Output: 3}, "hi"))
		rec := httptest.NewRecorder()
		s.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions",
			strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)))
		var resp struct {
			Usage struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Usage.PromptTokens != 7 || resp.Usage.CompletionTokens != 3 {
			t.Fatalf("usage = %+v", resp.Usage)
		}
	})

	t.Run("anthropic", func(t *testing.T) {
		s := serverFor(usageFlow(model.Usage{Input: 4, Output: 2}, "hi"))
		rec := httptest.NewRecorder()
		s.anthropic(rec, httptest.NewRequest("POST", "/v1/messages",
			strings.NewReader(`{"messages":[{"role":"user","content":"hi"}]}`)))
		var resp struct {
			Usage struct {
				InputTokens  int64 `json:"input_tokens"`
				OutputTokens int64 `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if resp.Usage.InputTokens != 4 || resp.Usage.OutputTokens != 2 {
			t.Fatalf("usage = %+v", resp.Usage)
		}
	})
}

// OpenAI's streaming terminal usage chunk is gated on the client actually
// having asked for it (stream_options.include_usage) — bb doesn't surprise a
// client that never opted in.
func TestServeOpenAIStreamUsageGatedByIncludeUsage(t *testing.T) {
	usageFlow := flow.New().WithAgent(agent.New().WithModel(
		model.Bound(&model.Mock{Chunks: []string{"hi"}, Usage: &model.Usage{Input: 9, Output: 1}}))).WithId("talk")

	withoutOptIn := serverFor(usageFlow)
	rec := httptest.NewRecorder()
	withoutOptIn.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"stream":true,"messages":[{"role":"user","content":"hi"}]}`)))
	if strings.Contains(rec.Body.String(), `"usage"`) {
		t.Fatalf("usage must not appear without an include_usage opt-in: %s", rec.Body.String())
	}

	withOptIn := serverFor(usageFlow)
	rec = httptest.NewRecorder()
	withOptIn.openai(rec, httptest.NewRequest("POST", "/v1/chat/completions",
		strings.NewReader(`{"stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`)))
	if !strings.Contains(rec.Body.String(), `"prompt_tokens":9`) {
		t.Fatalf("usage should appear with include_usage opted in: %s", rec.Body.String())
	}
}
