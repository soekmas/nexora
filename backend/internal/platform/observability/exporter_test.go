package observability_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"example.com/nexora/backend/internal/platform/observability"
)

func TestTraceExporter_Lifecycle(t *testing.T) {
	cfg := observability.DefaultExporterConfig()
	cfg.BatchTimeout = 50 * time.Millisecond
	exporter := observability.NewTraceExporter(cfg, nil)

	ctx := context.Background()
	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Double start fails
	if err := exporter.Start(ctx); err == nil {
		t.Fatalf("expected error on double start")
	}

	span := observability.Span{
		TraceID:    observability.GenerateTraceID(),
		SpanID:     observability.GenerateSpanID(),
		Name:       "TestSpan",
		StartTime:  time.Now().Add(-10 * time.Millisecond),
		EndTime:    time.Now(),
		StatusCode: 1,
	}

	if !exporter.ExportSpan(span) {
		t.Fatalf("expected ExportSpan to succeed")
	}

	if err := exporter.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if err := exporter.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Export after stop fails
	if exporter.ExportSpan(span) {
		t.Fatalf("expected ExportSpan after Stop to return false")
	}
}

func TestTraceExporter_HttpExport(t *testing.T) {
	var mu sync.Mutex
	var receivedBodies [][]byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST method, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content-type, got %s", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		receivedBodies = append(receivedBodies, body)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := observability.ExporterConfig{
		CollectorURL:   server.URL,
		ServiceName:    "test-service",
		ServiceVersion: "2.1.0",
		Environment:    "staging",
		BatchSize:      2,
		BatchTimeout:   50 * time.Millisecond,
		QueueSize:      100,
		ExportTimeout:  2 * time.Second,
	}

	exporter := observability.NewTraceExporter(cfg, nil)
	ctx := context.Background()
	if err := exporter.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	traceID := observability.GenerateTraceID()
	span1 := observability.Span{
		TraceID:    traceID,
		SpanID:     observability.GenerateSpanID(),
		Name:       "HTTP GET /api/v1/orders",
		Kind:       2, // SERVER
		StartTime:  time.Now().Add(-50 * time.Millisecond),
		EndTime:    time.Now(),
		Attributes: map[string]string{"http.status_code": "200"},
		StatusCode: 1, // OK
	}

	span2 := observability.Span{
		TraceID:    traceID,
		SpanID:     observability.GenerateSpanID(),
		Name:       "DB Query orders",
		Kind:       3, // CLIENT
		StartTime:  time.Now().Add(-20 * time.Millisecond),
		EndTime:    time.Now(),
		Attributes: map[string]string{"db.system": "postgresql"},
		StatusCode: 1,
	}

	exporter.ExportSpan(span1)
	exporter.ExportSpan(span2)

	// Stop flushes all pending spans
	if err := exporter.Stop(ctx); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(receivedBodies) == 0 {
		t.Fatalf("expected at least 1 HTTP request to collector, got 0")
	}

	var parsed map[string]any
	if err := json.Unmarshal(receivedBodies[0], &parsed); err != nil {
		t.Fatalf("failed to parse OTLP JSON payload: %v", err)
	}

	resSpans, ok := parsed["resourceSpans"].([]any)
	if !ok || len(resSpans) == 0 {
		t.Fatalf("expected non-empty resourceSpans")
	}

	if exporter.SentSpans() != 2 {
		t.Errorf("expected 2 sent spans, got %d", exporter.SentSpans())
	}
	if exporter.DroppedSpans() != 0 {
		t.Errorf("expected 0 dropped spans, got %d", exporter.DroppedSpans())
	}
}

func TestTraceExporter_BoundedQueueDrop(t *testing.T) {
	blockCh := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-blockCh
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := observability.ExporterConfig{
		CollectorURL:  server.URL,
		QueueSize:     5,
		BatchSize:     1, // First span triggers sendBatch which blocks on server
		BatchTimeout:  50 * time.Millisecond,
		ExportTimeout: 5 * time.Second,
	}

	exporter := observability.NewTraceExporter(cfg, nil)
	_ = exporter.Start(context.Background())

	// Span 0: triggers sendBatch and blocks worker
	exporter.ExportSpan(observability.Span{
		TraceID: observability.GenerateTraceID(),
		SpanID:  observability.GenerateSpanID(),
		Name:    "BlockingSpan",
	})

	// Wait briefly for worker to pick up Span 0 and block in sendBatch
	time.Sleep(30 * time.Millisecond)

	// Now worker is blocked. Fill the 5-slot queue buffer:
	for i := 0; i < 5; i++ {
		span := observability.Span{
			TraceID: observability.GenerateTraceID(),
			SpanID:  observability.GenerateSpanID(),
			Name:    "BufferedSpan",
		}
		if !exporter.ExportSpan(span) {
			t.Fatalf("expected span %d to be accepted into buffer", i)
		}
	}

	// 6th span must drop without blocking
	overflowSpan := observability.Span{
		TraceID: observability.GenerateTraceID(),
		SpanID:  observability.GenerateSpanID(),
		Name:    "OverflowSpan",
	}
	if exporter.ExportSpan(overflowSpan) {
		t.Fatalf("expected overflow span to be dropped")
	}

	if exporter.DroppedSpans() < 1 {
		t.Errorf("expected at least 1 dropped span, got %d", exporter.DroppedSpans())
	}

	// Unblock server so exporter can shutdown cleanly
	close(blockCh)
	_ = exporter.Stop(context.Background())
}

func TestTraceExporter_NoopMode(t *testing.T) {
	cfg := observability.DefaultExporterConfig()
	cfg.CollectorURL = "" // No-op mode
	cfg.BatchTimeout = 10 * time.Millisecond

	exporter := observability.NewTraceExporter(cfg, nil)
	ctx := context.Background()
	_ = exporter.Start(ctx)

	span := observability.Span{
		TraceID: observability.GenerateTraceID(),
		SpanID:  observability.GenerateSpanID(),
		Name:    "NoopSpan",
	}

	if !exporter.ExportSpan(span) {
		t.Fatalf("expected ExportSpan to succeed in no-op mode")
	}

	if err := exporter.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	if exporter.SentSpans() != 1 {
		t.Errorf("expected 1 sent span in no-op mode, got %d", exporter.SentSpans())
	}

	_ = exporter.Stop(ctx)
}
