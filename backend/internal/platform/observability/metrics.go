// Package observability provides bounded-cardinality metrics, Prometheus exposition, and tracing context.
package observability

import (
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultDurationBuckets defines standard duration histogram buckets in seconds for HTTP latencies.
var DefaultDurationBuckets = []float64{
	0.005,  // 5ms
	0.010,  // 10ms
	0.025,  // 25ms
	0.050,  // 50ms
	0.100,  // 100ms
	0.250,  // 250ms
	0.500,  // 500ms
	1.000,  // 1s
	2.500,  // 2.5s
	5.000,  // 5s
	10.000, // 10s
}

// Counter represents a monotonically increasing 64-bit unsigned integer metric.
type Counter struct {
	val uint64
}

// Inc increments the counter by 1.
func (c *Counter) Inc() {
	atomic.AddUint64(&c.val, 1)
}

// Add increases the counter by delta.
func (c *Counter) Add(delta uint64) {
	atomic.AddUint64(&c.val, delta)
}

// Value returns the current counter value.
func (c *Counter) Value() uint64 {
	return atomic.LoadUint64(&c.val)
}

// Gauge represents a numerical value that can arbitrarily go up and down.
type Gauge struct {
	val int64
}

// Set sets the gauge to val.
func (g *Gauge) Set(val int64) {
	atomic.StoreInt64(&g.val, val)
}

// Inc increments the gauge by 1.
func (g *Gauge) Inc() {
	atomic.AddInt64(&g.val, 1)
}

// Dec decrements the gauge by 1.
func (g *Gauge) Dec() {
	atomic.AddInt64(&g.val, -1)
}

// Value returns the current gauge value.
func (g *Gauge) Value() int64 {
	return atomic.LoadInt64(&g.val)
}

// Histogram tracks the distribution of observed values across bounded buckets.
type Histogram struct {
	mu      sync.RWMutex
	buckets []float64
	counts  []uint64
	count   uint64
	sum     float64
}

// NewHistogram creates a new Histogram with specified upper bucket limits.
func NewHistogram(buckets []float64) *Histogram {
	b := make([]float64, len(buckets))
	copy(b, buckets)
	sort.Float64s(b)
	return &Histogram{
		buckets: b,
		counts:  make([]uint64, len(b)),
	}
}

// Observe records a new observation into the histogram.
func (h *Histogram) Observe(val float64) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.count++
	h.sum += val

	for i, upper := range h.buckets {
		if val <= upper {
			h.counts[i]++
		}
	}
}

// Snapshot returns a copy of the histogram's current state.
func (h *Histogram) Snapshot() (buckets []float64, counts []uint64, count uint64, sum float64) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	bCopy := make([]float64, len(h.buckets))
	copy(bCopy, h.buckets)

	cCopy := make([]uint64, len(h.counts))
	copy(cCopy, h.counts)

	return bCopy, cCopy, h.count, h.sum
}

// DBPoolStats returns connection pool statistics.
type DBPoolStats struct {
	TotalConns    int
	AcquiredConns int
	IdleConns     int
	MaxConns      int
}

// DBPoolStatsFunc is a callback that inspects active database pool statistics.
type DBPoolStatsFunc func() DBPoolStats

// Metrics is the central in-memory registry for platform and operational metrics.
type Metrics struct {
	mu        sync.RWMutex
	startTime time.Time

	// HTTP metrics
	httpRequestsInFlight Gauge
	httpRequestsTotal    map[string]*Counter   // key: method + "|" + route + "|" + status_class
	httpRequestDuration  map[string]*Histogram // key: method + "|" + route

	// Worker metrics
	workerActive        Gauge
	workerDispatchTotal map[string]*Counter // key: status
	workerEventsClaimed Counter
	workerPollCycles    Counter

	// DB pool stats callback
	dbPoolStatsFn DBPoolStatsFunc
}

// NewMetrics initializes an empty metrics registry.
func NewMetrics() *Metrics {
	return &Metrics{
		startTime:           time.Now(),
		httpRequestsTotal:   make(map[string]*Counter),
		httpRequestDuration: make(map[string]*Histogram),
		workerDispatchTotal: make(map[string]*Counter),
	}
}

// NormalizeMethod restricts HTTP method names to standard bounded uppercase verbs.
func NormalizeMethod(m string) string {
	switch strings.ToUpper(strings.TrimSpace(m)) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions:
		return strings.ToUpper(strings.TrimSpace(m))
	default:
		return "OTHER"
	}
}

// NormalizeRoute restricts route paths to templates, preventing unbounded cardinality from IDs or queries.
func NormalizeRoute(r string) string {
	trimmed := strings.TrimSpace(r)
	if trimmed == "" || trimmed == "/" {
		return "/"
	}
	// If route contains query params, strip them
	if idx := strings.IndexByte(trimmed, '?'); idx != -1 {
		trimmed = trimmed[:idx]
	}
	// Prevent unbounded cardinality: if route contains excessive path length or spaces, collapse
	if len(trimmed) > 120 || strings.Contains(trimmed, " ") {
		return "/unmatched"
	}
	return trimmed
}

// NormalizeStatusClass maps HTTP status codes to bounded 1xx..5xx classes.
func NormalizeStatusClass(code int) string {
	switch {
	case code >= 100 && code < 200:
		return "1xx"
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "unknown"
	}
}

// NormalizeWorkerStatus restricts worker dispatch outcomes to bounded keywords.
func NormalizeWorkerStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success":
		return "success"
	case "retry":
		return "retry"
	case "dead_letter":
		return "dead_letter"
	default:
		return "failed"
	}
}

// RecordHTTPRequest records an HTTP request outcome into counters and latency histograms.
func (m *Metrics) RecordHTTPRequest(method string, route string, statusCode int, duration time.Duration) {
	normMethod := NormalizeMethod(method)
	normRoute := NormalizeRoute(route)
	normClass := NormalizeStatusClass(statusCode)

	// Counter
	key := normMethod + "|" + normRoute + "|" + normClass
	m.mu.Lock()
	c, ok := m.httpRequestsTotal[key]
	if !ok {
		c = &Counter{}
		m.httpRequestsTotal[key] = c
	}
	// Histogram
	histKey := normMethod + "|" + normRoute
	h, ok := m.httpRequestDuration[histKey]
	if !ok {
		h = NewHistogram(DefaultDurationBuckets)
		m.httpRequestDuration[histKey] = h
	}
	m.mu.Unlock()

	c.Inc()
	h.Observe(duration.Seconds())
}

// IncHTTPInFlight increments the active in-flight HTTP request gauge.
func (m *Metrics) IncHTTPInFlight() {
	m.httpRequestsInFlight.Inc()
}

// DecHTTPInFlight decrements the active in-flight HTTP request gauge.
func (m *Metrics) DecHTTPInFlight() {
	m.httpRequestsInFlight.Dec()
}

// HTTPInFlight returns currently active in-flight HTTP requests.
func (m *Metrics) HTTPInFlight() int64 {
	return m.httpRequestsInFlight.Value()
}

// RecordWorkerClaim increments the count of outbox events claimed by worker.
func (m *Metrics) RecordWorkerClaim(count int) {
	if count > 0 {
		m.workerEventsClaimedCounter().Add(uint64(count))
	}
}

// RecordWorkerDispatch records a worker event dispatch outcome.
func (m *Metrics) RecordWorkerDispatch(status string) {
	norm := NormalizeWorkerStatus(status)
	m.mu.Lock()
	c, ok := m.workerDispatchTotal[norm]
	if !ok {
		c = &Counter{}
		m.workerDispatchTotal[norm] = c
	}
	m.mu.Unlock()
	c.Inc()
}

// RecordWorkerPollCycle increments the worker polling cycle counter.
func (m *Metrics) RecordWorkerPollCycle() {
	m.workerPollCycles.Inc()
}

// SetWorkerActive marks whether the worker is currently running.
func (m *Metrics) SetWorkerActive(active bool) {
	if active {
		m.workerActive.Set(1)
	} else {
		m.workerActive.Set(0)
	}
}

func (m *Metrics) workerEventsClaimedCounter() *Counter {
	return &m.workerEventsClaimed
}

// SetDBPoolStatsFunc registers a callback to gather database connection pool statistics dynamically.
func (m *Metrics) SetDBPoolStatsFunc(fn DBPoolStatsFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dbPoolStatsFn = fn
}

// WritePrometheus serializes all registered metrics into the standard Prometheus exposition format v0.0.4.
func (m *Metrics) WritePrometheus(w io.Writer) error {
	var sb strings.Builder

	// 1. Process uptime and Go runtime metrics
	uptime := time.Since(m.startTime).Seconds()
	sb.WriteString("# HELP process_uptime_seconds Total seconds since process startup\n")
	sb.WriteString("# TYPE process_uptime_seconds gauge\n")
	sb.WriteString("process_uptime_seconds " + strconv.FormatFloat(uptime, 'f', 3, 64) + "\n\n")

	sb.WriteString("# HELP go_goroutines Number of active goroutines\n")
	sb.WriteString("# TYPE go_goroutines gauge\n")
	sb.WriteString("go_goroutines " + strconv.Itoa(runtime.NumGoroutine()) + "\n\n")

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	sb.WriteString("# HELP go_memstats_alloc_bytes Number of bytes allocated on the Go heap\n")
	sb.WriteString("# TYPE go_memstats_alloc_bytes gauge\n")
	sb.WriteString("go_memstats_alloc_bytes " + strconv.FormatUint(memStats.Alloc, 10) + "\n\n")

	// 2. HTTP In-Flight
	sb.WriteString("# HELP http_requests_in_flight Number of currently active in-flight HTTP requests\n")
	sb.WriteString("# TYPE http_requests_in_flight gauge\n")
	sb.WriteString("http_requests_in_flight " + strconv.FormatInt(m.httpRequestsInFlight.Value(), 10) + "\n\n")

	// 3. HTTP Requests Total
	m.mu.RLock()
	var httpKeys []string
	for k := range m.httpRequestsTotal {
		httpKeys = append(httpKeys, k)
	}
	sort.Strings(httpKeys)

	sb.WriteString("# HELP http_requests_total Total number of HTTP requests processed\n")
	sb.WriteString("# TYPE http_requests_total counter\n")
	for _, k := range httpKeys {
		parts := strings.Split(k, "|")
		if len(parts) == 3 {
			val := m.httpRequestsTotal[k].Value()
			fmt.Fprintf(&sb, "http_requests_total{method=%q,route=%q,status_class=%q} %d\n", parts[0], parts[1], parts[2], val)
		}
	}
	sb.WriteString("\n")

	// 4. HTTP Request Duration Histogram
	var histKeys []string
	for k := range m.httpRequestDuration {
		histKeys = append(histKeys, k)
	}
	sort.Strings(histKeys)

	sb.WriteString("# HELP http_request_duration_seconds HTTP request execution duration in seconds\n")
	sb.WriteString("# TYPE http_request_duration_seconds histogram\n")
	for _, k := range histKeys {
		parts := strings.Split(k, "|")
		if len(parts) == 2 {
			method, route := parts[0], parts[1]
			h := m.httpRequestDuration[k]
			buckets, counts, count, sum := h.Snapshot()

			for i, upper := range buckets {
				upperStr := strconv.FormatFloat(upper, 'f', -1, 64)
				fmt.Fprintf(&sb, "http_request_duration_seconds_bucket{method=%q,route=%q,le=%q} %d\n",
					method, route, upperStr, counts[i])
			}
			fmt.Fprintf(&sb, "http_request_duration_seconds_bucket{method=%q,route=%q,le=\"+Inf\"} %d\n",
				method, route, count)
			fmt.Fprintf(&sb, "http_request_duration_seconds_sum{method=%q,route=%q} %s\n",
				method, route, strconv.FormatFloat(sum, 'f', -1, 64))
			fmt.Fprintf(&sb, "http_request_duration_seconds_count{method=%q,route=%q} %d\n",
				method, route, count)
		}
	}
	sb.WriteString("\n")

	// 5. Worker metrics
	sb.WriteString("# HELP worker_active Gauge indicating if the background worker daemon is active (1=active, 0=inactive)\n")
	sb.WriteString("# TYPE worker_active gauge\n")
	sb.WriteString("worker_active " + strconv.FormatInt(m.workerActive.Value(), 10) + "\n\n")

	sb.WriteString("# HELP worker_events_claimed_total Total number of outbox events claimed by background worker\n")
	sb.WriteString("# TYPE worker_events_claimed_total counter\n")
	sb.WriteString("worker_events_claimed_total " + strconv.FormatUint(m.workerEventsClaimed.Value(), 10) + "\n\n")

	sb.WriteString("# HELP worker_poll_cycles_total Total number of worker polling loop executions\n")
	sb.WriteString("# TYPE worker_poll_cycles_total counter\n")
	sb.WriteString("worker_poll_cycles_total " + strconv.FormatUint(m.workerPollCycles.Value(), 10) + "\n\n")

	var workerKeys []string
	for k := range m.workerDispatchTotal {
		workerKeys = append(workerKeys, k)
	}
	sort.Strings(workerKeys)

	sb.WriteString("# HELP worker_dispatch_total Total number of worker dispatch outcomes by status\n")
	sb.WriteString("# TYPE worker_dispatch_total counter\n")
	for _, k := range workerKeys {
		val := m.workerDispatchTotal[k].Value()
		fmt.Fprintf(&sb, "worker_dispatch_total{status=%q} %d\n", k, val)
	}
	sb.WriteString("\n")

	// 6. DB Pool metrics if configured
	dbFn := m.dbPoolStatsFn
	m.mu.RUnlock()

	if dbFn != nil {
		stats := dbFn()
		sb.WriteString("# HELP db_pool_total_conns Total number of connections currently in the database pool\n")
		sb.WriteString("# TYPE db_pool_total_conns gauge\n")
		sb.WriteString("db_pool_total_conns " + strconv.Itoa(stats.TotalConns) + "\n\n")

		sb.WriteString("# HELP db_pool_acquired_conns Number of connections currently checked out / active\n")
		sb.WriteString("# TYPE db_pool_acquired_conns gauge\n")
		sb.WriteString("db_pool_acquired_conns " + strconv.Itoa(stats.AcquiredConns) + "\n\n")

		sb.WriteString("# HELP db_pool_idle_conns Number of idle connections in the pool\n")
		sb.WriteString("# TYPE db_pool_idle_conns gauge\n")
		sb.WriteString("db_pool_idle_conns " + strconv.Itoa(stats.IdleConns) + "\n\n")

		sb.WriteString("# HELP db_pool_max_conns Maximum number of connections configured for the pool\n")
		sb.WriteString("# TYPE db_pool_max_conns gauge\n")
		sb.WriteString("db_pool_max_conns " + strconv.Itoa(stats.MaxConns) + "\n\n")
	}

	_, err := io.WriteString(w, sb.String())
	return err
}

// Handler returns an http.Handler that responds to GET /metrics requests with Prometheus text exposition.
func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = m.WritePrometheus(w)
	})
}
