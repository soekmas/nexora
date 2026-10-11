// Package observability provides metrics, structured logging, distributed tracing, and OTLP export.
package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Span represents a single tracing unit of work.
type Span struct {
	TraceID      string            `json:"trace_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id,omitempty"`
	Name         string            `json:"name"`
	Kind         int               `json:"kind"` // 1=INTERNAL, 2=SERVER, 3=CLIENT
	StartTime    time.Time         `json:"start_time"`
	EndTime      time.Time         `json:"end_time"`
	Attributes   map[string]string `json:"attributes,omitempty"`
	StatusCode   int               `json:"status_code,omitempty"` // 0=UNSET, 1=OK, 2=ERROR
	StatusMsg    string            `json:"status_msg,omitempty"`
}

// ExporterConfig configures the lightweight OTLP HTTP trace exporter.
type ExporterConfig struct {
	CollectorURL   string            // Target URL (e.g. "http://localhost:4318/v1/traces")
	ServiceName    string            // Default: "nexora-platform"
	ServiceVersion string            // Default: "1.0.0"
	Environment    string            // Default: "development"
	Attributes     map[string]string // Custom resource attributes
	BatchSize      int               // Max spans per batch (default 50)
	BatchTimeout   time.Duration     // Max duration before batch flush (default 500ms)
	QueueSize      int               // Bounded queue buffer (default 1000)
	ExportTimeout  time.Duration     // HTTP export request timeout (default 3s)
	HTTPClient     *http.Client      // Custom HTTP client (optional)
}

// DefaultExporterConfig returns safe production defaults.
func DefaultExporterConfig() ExporterConfig {
	return ExporterConfig{
		CollectorURL:   "",
		ServiceName:    "nexora-platform",
		ServiceVersion: "1.0.0",
		Environment:    "development",
		BatchSize:      50,
		BatchTimeout:   500 * time.Millisecond,
		QueueSize:      1000,
		ExportTimeout:  3 * time.Second,
	}
}

// TraceExporter batches and exports spans to an OTLP-compliant HTTP collector
// using a non-blocking bounded queue and clean shutdown semantics.
type TraceExporter struct {
	cfg        ExporterConfig
	client     *http.Client
	queue      chan Span
	flushCh    chan chan struct{}
	stopCh     chan struct{}
	doneCh     chan struct{}
	logger     *slog.Logger
	running    atomic.Bool
	droppedCnt atomic.Int64
	sentCnt    atomic.Int64
	mu         sync.Mutex
}

// NewTraceExporter constructs a new TraceExporter.
func NewTraceExporter(cfg ExporterConfig, logger *slog.Logger) *TraceExporter {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "nexora-platform"
	}
	if cfg.ServiceVersion == "" {
		cfg.ServiceVersion = "1.0.0"
	}
	if cfg.Environment == "" {
		cfg.Environment = "development"
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 50
	} else if cfg.BatchSize > 500 {
		cfg.BatchSize = 500
	}
	if cfg.BatchTimeout <= 0 {
		cfg.BatchTimeout = 500 * time.Millisecond
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1000
	}
	if cfg.ExportTimeout <= 0 {
		cfg.ExportTimeout = 3 * time.Second
	}

	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: cfg.ExportTimeout,
		}
	}

	return &TraceExporter{
		cfg:     cfg,
		client:  client,
		queue:   make(chan Span, cfg.QueueSize),
		flushCh: make(chan chan struct{}),
		stopCh:  make(chan struct{}),
		doneCh:  make(chan struct{}),
		logger:  logger,
	}
}

// Start launches the background worker loop.
func (e *TraceExporter) Start(_ context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running.Load() {
		return errors.New("trace exporter is already running")
	}

	e.running.Store(true)
	e.stopCh = make(chan struct{})
	e.doneCh = make(chan struct{})

	go e.workerLoop()
	return nil
}

// ExportSpan enqueues a span for export.
// It is non-blocking: if the queue is full, the span is dropped and
// the dropped counter is incremented, protecting application latency.
func (e *TraceExporter) ExportSpan(span Span) bool {
	if !e.running.Load() {
		return false
	}
	if span.TraceID == "" || span.SpanID == "" {
		return false
	}

	select {
	case e.queue <- span:
		return true
	default:
		e.droppedCnt.Add(1)
		return false
	}
}

// DroppedSpans returns the total count of spans dropped due to queue saturation.
func (e *TraceExporter) DroppedSpans() int64 {
	return e.droppedCnt.Load()
}

// SentSpans returns the total count of spans successfully exported.
func (e *TraceExporter) SentSpans() int64 {
	return e.sentCnt.Load()
}

// Flush signals the background worker to flush any pending spans and waits for completion.
func (e *TraceExporter) Flush(ctx context.Context) error {
	if !e.running.Load() {
		return nil
	}

	ack := make(chan struct{})
	select {
	case e.flushCh <- ack:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case <-ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Stop gracefully shuts down the exporter, draining remaining spans and flushing them.
func (e *TraceExporter) Stop(ctx context.Context) error {
	e.mu.Lock()
	if !e.running.Load() {
		e.mu.Unlock()
		return nil
	}
	e.running.Store(false)
	close(e.stopCh)
	e.mu.Unlock()

	select {
	case <-e.doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *TraceExporter) workerLoop() {
	defer close(e.doneCh)

	ticker := time.NewTicker(e.cfg.BatchTimeout)
	defer ticker.Stop()

	batch := make([]Span, 0, e.cfg.BatchSize)

	flushCurrent := func() {
		if len(batch) > 0 {
			e.sendBatch(batch)
			batch = batch[:0]
		}
	}

	for {
		select {
		case <-e.stopCh:
			// Drain remaining spans from queue
			for {
				select {
				case s := <-e.queue:
					batch = append(batch, s)
					if len(batch) >= e.cfg.BatchSize {
						flushCurrent()
					}
				default:
					flushCurrent()
					return
				}
			}

		case ack := <-e.flushCh:
			// Drain all currently queued spans then ack
			draining := true
			for draining {
				select {
				case s := <-e.queue:
					batch = append(batch, s)
					if len(batch) >= e.cfg.BatchSize {
						flushCurrent()
					}
				default:
					flushCurrent()
					close(ack)
					draining = false
				}
			}

		case s := <-e.queue:
			batch = append(batch, s)
			if len(batch) >= e.cfg.BatchSize {
				flushCurrent()
			}

		case <-ticker.C:
			flushCurrent()
		}
	}
}

func (e *TraceExporter) sendBatch(spans []Span) {
	if len(spans) == 0 {
		return
	}

	// No-op mode when no collector URL is configured
	if e.cfg.CollectorURL == "" {
		e.sentCnt.Add(int64(len(spans)))
		return
	}

	payload, err := e.buildOTLPPayload(spans)
	if err != nil {
		e.logger.Error("Failed to build OTLP payload", slog.String("error", err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), e.cfg.ExportTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.cfg.CollectorURL, bytes.NewReader(payload))
	if err != nil {
		e.logger.Error("Failed to create OTLP HTTP request", slog.String("error", err.Error()))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		e.logger.Warn("Failed to export OTLP traces",
			slog.String("collector", e.cfg.CollectorURL),
			slog.Int("spans", len(spans)),
			slog.String("error", err.Error()),
		)
		return
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		e.sentCnt.Add(int64(len(spans)))
	} else {
		e.logger.Warn("OTLP collector returned non-2xx response",
			slog.String("collector", e.cfg.CollectorURL),
			slog.Int("status", resp.StatusCode),
			slog.Int("spans", len(spans)),
		)
	}
}

// OTLP JSON wire structures according to OpenTelemetry specification.
type otlpExportRequest struct {
	ResourceSpans []otlpResourceSpans `json:"resourceSpans"`
}

type otlpResourceSpans struct {
	Resource   otlpResource     `json:"resource"`
	ScopeSpans []otlpScopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpKeyValue `json:"attributes"`
}

type otlpScopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type otlpSpan struct {
	TraceID           string         `json:"traceId"`
	SpanID            string         `json:"spanId"`
	ParentSpanID      string         `json:"parentSpanId,omitempty"`
	Name              string         `json:"name"`
	Kind              int            `json:"kind"`
	StartTimeUnixNano string         `json:"startTimeUnixNano"`
	EndTimeUnixNano   string         `json:"endTimeUnixNano"`
	Attributes        []otlpKeyValue `json:"attributes,omitempty"`
	Status            otlpStatus     `json:"status"`
}

type otlpKeyValue struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpAnyValue struct {
	StringValue string `json:"stringValue"`
}

type otlpStatus struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

func (e *TraceExporter) buildOTLPPayload(spans []Span) ([]byte, error) {
	resAttrs := []otlpKeyValue{
		{Key: "service.name", Value: otlpAnyValue{StringValue: e.cfg.ServiceName}},
		{Key: "service.version", Value: otlpAnyValue{StringValue: e.cfg.ServiceVersion}},
		{Key: "deployment.environment", Value: otlpAnyValue{StringValue: e.cfg.Environment}},
	}
	for k, v := range e.cfg.Attributes {
		resAttrs = append(resAttrs, otlpKeyValue{Key: k, Value: otlpAnyValue{StringValue: v}})
	}

	otlpSpans := make([]otlpSpan, len(spans))
	for i, s := range spans {
		startNano := s.StartTime.UnixNano()
		endNano := s.EndTime.UnixNano()
		if endNano <= startNano {
			endNano = startNano + int64(time.Millisecond)
		}

		var spanAttrs []otlpKeyValue
		for k, v := range s.Attributes {
			spanAttrs = append(spanAttrs, otlpKeyValue{Key: k, Value: otlpAnyValue{StringValue: v}})
		}

		kind := s.Kind
		if kind <= 0 {
			kind = 1 // SPAN_KIND_INTERNAL
		}

		otlpSpans[i] = otlpSpan{
			TraceID:           s.TraceID,
			SpanID:            s.SpanID,
			ParentSpanID:      s.ParentSpanID,
			Name:              s.Name,
			Kind:              kind,
			StartTimeUnixNano: strconv.FormatInt(startNano, 10),
			EndTimeUnixNano:   strconv.FormatInt(endNano, 10),
			Attributes:        spanAttrs,
			Status: otlpStatus{
				Code:    s.StatusCode,
				Message: s.StatusMsg,
			},
		}
	}

	req := otlpExportRequest{
		ResourceSpans: []otlpResourceSpans{
			{
				Resource: otlpResource{Attributes: resAttrs},
				ScopeSpans: []otlpScopeSpans{
					{
						Scope: otlpScope{
							Name:    "example.com/nexora",
							Version: e.cfg.ServiceVersion,
						},
						Spans: otlpSpans,
					},
				},
			},
		},
	}

	return json.Marshal(req)
}
