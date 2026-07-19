package model

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMonitoredDelegatesAndRecords(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	t.Cleanup(func() { otel.SetMeterProvider(noop.NewMeterProvider()) })

	mock := &Mock{Chunks: []string{"a", "b"}}
	m := Monitored(mock, "gpt-test")
	stream, err := m.Stream(context.Background(), []Message{{Role: "user", Content: "hi"}}, Params{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	got, err := Collect(stream)
	if err != nil || got != "ab" {
		t.Fatalf("delegation broken: %q, %v", got, err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	names := map[string]bool{}
	for _, sm := range rm.ScopeMetrics {
		for _, met := range sm.Metrics {
			names[met.Name] = true
		}
	}
	for _, want := range []string{"model.calls", "model.call.seconds", "model.chunks"} {
		if !names[want] {
			t.Fatalf("metric %q not recorded; got %v", want, names)
		}
	}
}

func TestMonitoredPropagatesRejection(t *testing.T) {
	boom := errors.New("boom")
	m := Monitored(&Mock{Reject: boom}, "gpt-test")
	if _, err := m.Stream(context.Background(), nil, Params{}); !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}
}

func TestMonitoredPropagatesStreamError(t *testing.T) {
	boom := errors.New("boom")
	m := Monitored(&Mock{Chunks: []string{"a"}, Fail: boom}, "gpt-test")
	stream, err := m.Stream(context.Background(), nil, Params{})
	if err != nil {
		t.Fatal(err)
	}
	got, err := Collect(stream)
	if !errors.Is(err, boom) || got != "a" {
		t.Fatalf("got %q, %v", got, err)
	}
}
