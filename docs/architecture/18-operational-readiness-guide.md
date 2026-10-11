# NEXORA — Operational Readiness & Observability Guide

## 1. Overview & Operational Architecture

This document defines the operational architecture, observability contracts, probe semantics, and production readiness guidelines for the **NEXORA Retail Business Platform** (Phase 1 Foundation).

The operational design adheres strictly to the non-negotiable architecture locks:
- **Zero Secret Leakage:** Absolute prohibition of sensitive credential, token, CVV, or PII persistence in logs, error payloads, metrics, or diagnostic endpoints.
- **Bounded Cardinality:** Metrics labels are strictly normalized and bounded to prevent memory leaks and timeseries explosion.
- **Decoupled Probe Semantics:** Liveness (`/healthz`) and readiness (`/readyz`) are decoupled to prevent infrastructure cascading failures.
- **Graceful Lifecycle Management:** Explicit startup/shutdown orchestration with bounded context cancellation across HTTP servers and background workers.
- **Canonical Error Envelope:** All transport errors, probe failures, and middleware rejections adhere to the unified `ErrorResponse` schema.

---

## 2. Structured Logging & Secret Redaction

### 2.1 Standardized Schema (`log/slog`)

All components (API, Worker, and Platform Subsystems) use structured logging via Go's standard `log/slog` library with JSON formatting in production environments and text formatting in local development.

Every log entry includes:
| Field | Type | Description | Example |
| :--- | :--- | :--- | :--- |
| `time` | RFC3339 | Event timestamp | `2026-10-10T21:45:00.000Z` |
| `level` | String | Log severity (`DEBUG`, `INFO`, `WARN`, `ERROR`) | `INFO` |
| `service` | String | Component identifier | `nexora-api`, `nexora-worker` |
| `env` | String | Deployment environment | `production`, `staging`, `testing` |
| `msg` | String | Human-readable log message | `HTTP request completed` |
| `request_id` | String | Correlation ID for HTTP request | `req_1728593100123456789` |
| `trace_id` | String | W3C distributed trace identifier | `4bf92f3577b34da6a3ce929d0e0e4736` |
| `route` | String | Parameterized chi route template (never raw path) | `/api/v1/orders/{order_id}` |
| `method` | String | HTTP verb | `GET`, `POST`, `PUT`, `DELETE` |
| `status` | Integer | HTTP response status code | `200`, `400`, `500` |
| `duration_ms` | Float | Request execution duration | `12.45` |

### 2.2 Route Template Logging (Anti-Cardinaity & Anti-Leakage)

HTTP logging middleware logs `rctx.RoutePattern()` rather than `r.URL.Path`. This prevents:
1. High-entropy UUIDs or identifiers from polluting log index cardinality.
2. Accidental leakage of sensitive URL path parameters (e.g. reset tokens, phone numbers).
3. If no route pattern is matched (404), the route is classified as `_unmatched`.

### 2.3 Automated Attribute Redaction (`RedactAttr`)

The platform implements a centralized `RedactAttr` function registered directly into `slog.HandlerOptions.ReplaceAttr`.

#### Protected Keys
Any attribute key matching (case-insensitively, with underscores/hyphens normalized) the following list is automatically replaced with `"[REDACTED]"`:
`authorization`, `token`, `access_token`, `refresh_token`, `session_token`, `jwt`, `bearer`, `password`, `secret`, `api_key`, `apikey`, `private_key`, `cookie`, `set-cookie`, `card_number`, `pan`, `cvv`, `cvc`, `pin`.

#### Pattern-Based Value Masking
1. **Authorization Headers / Bearer Tokens:** Values matching `Bearer <token>` or `Basic <token>` are masked to `Bearer [REDACTED]` or `Basic [REDACTED]`.
2. **Database DSNs / Connection Strings:** URIs of the form `postgres://user:password@host...` have credentials replaced with `[REDACTED]`, outputting `postgres://user:[REDACTED]@host...`.

---

## 3. Bounded-Cardinality Metrics & Prometheus Exposition

The platform implements a high-performance, thread-safe metrics engine (`backend/internal/platform/observability/metrics.go`) without external runtime dependencies. Metrics are exposed via standard Prometheus text format (`text/plain; version=0.0.4; charset=utf-8`) at `GET /metrics`.

### 3.1 Strict Cardinality Bounding Rules

1. **Normalized Methods:** HTTP methods are strictly normalized to known verbs (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`). Any other verb is labeled `OTHER`.
2. **Normalized Routes:** Only parameterized route templates are used (e.g., `/healthz`, `/api/v1/orders/{order_id}`). Unmatched routes are recorded as `_unmatched`.
3. **Status Classes:** HTTP status codes are converted to coarse status classes (`2xx`, `3xx`, `4xx`, `5xx`).
4. **Worker Status Classes:** Worker outcomes are normalized to `success`, `retry`, `dead_letter`, and `failure`.
5. **Prohibited Labels:** Never use `request_id`, `trace_id`, `tenant_id`, `user_id`, raw URLs, or arbitrary error messages as metric labels.

### 3.2 Metrics Catalog

#### HTTP Server Metrics
| Metric Name | Type | Labels | Description |
| :--- | :--- | :--- | :--- |
| `http_requests_total` | Counter | `method`, `route`, `status_class` | Cumulative count of handled HTTP requests. |
| `http_requests_in_flight` | Gauge | None | Number of HTTP requests currently executing. |
| `http_request_duration_seconds` | Histogram | `method`, `route` | Latency distribution with buckets: `[0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]`. |

#### Background Worker & Outbox Metrics
| Metric Name | Type | Labels | Description |
| :--- | :--- | :--- | :--- |
| `worker_active` | Gauge | None | Boolean indicator (1=running, 0=stopped) of worker daemon. |
| `worker_poll_cycles_total` | Counter | None | Total count of outbox poller execution cycles. |
| `worker_events_claimed_total` | Counter | None | Total number of transactional outbox events successfully claimed. |
| `worker_dispatch_total` | Counter | `status` (`success`, `retry`, `dead_letter`, `failure`) | Total dispatch outcomes by terminal category. |

#### Database Connection Pool Metrics
| Metric Name | Type | Labels | Description |
| :--- | :--- | :--- | :--- |
| `db_pool_total_conns` | Gauge | None | Total connections currently established in PostgreSQL pool. |
| `db_pool_idle_conns` | Gauge | None | Current idle connections in PostgreSQL pool. |
| `db_pool_acquired_conns` | Gauge | None | Current acquired (checked-out) connections in PostgreSQL pool. |

#### Process & Go Runtime Metrics
| Metric Name | Type | Labels | Description |
| :--- | :--- | :--- | :--- |
| `process_uptime_seconds` | Gauge | None | Service uptime in seconds since boot. |
| `go_goroutines` | Gauge | None | Current number of active Go goroutines. |
| `go_memstats_alloc_bytes` | Gauge | None | Current allocated heap memory bytes. |

### 3.3 Per-Instance Prometheus Scraping Architecture

Because metrics counters, histograms, and pool gauges are maintained in-memory per service process, **Prometheus must scrape each pod/instance directly** rather than scraping through a load-balanced endpoint:

1. **Why Scrape Per-Instance:**
   - Scraping a load-balanced endpoint causes non-deterministic sampling across instances, corrupting rates (`rate()`) and counter monotonicity.
   - Gauges like `db_pool_acquired_conns` and `http_requests_in_flight` represent node-local state that must be summed or averaged across instance targets in PromQL.
2. **Kubernetes Discovery Configuration:**
   Use `kubernetes_sd_configs` with `role: pod` targeting port 8080:
   ```yaml
   scrape_configs:
     - job_name: 'nexora-api'
       scrape_interval: 15s
       scrape_timeout: 10s
       metrics_path: '/metrics'
       kubernetes_sd_configs:
         - role: pod
           namespaces:
             names: ['nexora-prod']
       relabel_configs:
         - source_labels: [__meta_kubernetes_pod_label_app]
           action: keep
           regex: nexora-api
         - source_labels: [__meta_kubernetes_pod_ip]
           target_label: __address__
           replacement: '${1}:8080'
         - source_labels: [__meta_kubernetes_pod_name]
           target_label: instance
   ```
3. **Cross-Instance Aggregation in PromQL:**
   - Total cluster request rate: `sum(rate(http_requests_total[5m]))`
   - Cluster-wide P99 latency: `histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))`
   - Cluster-wide active DB connections: `sum(db_pool_acquired_conns)`

---

## 4. Health Probes: Liveness vs. Readiness Semantics

Probes are decoupled to comply with Kubernetes and container orchestration best practices.

### 4.1 Liveness Probe (`GET /healthz`)
- **Semantics:** Indicates whether the Go process and HTTP listener are alive and responsive.
- **Dependency Isolation:** `/healthz` **never** checks external dependencies (PostgreSQL or Redis). If the database is experiencing an outage or high latency, `/healthz` continues to return `200 OK`.
- **Purpose:** Prevents container restart cascades during downstream database failover or maintenance windows.
- **Response:**
  ```json
  {
    "status": "ok"
  }
  ```

### 4.2 Readiness Probe (`GET /readyz`)
- **Semantics:** Indicates whether the service instance is ready to receive and process user traffic.
- **Dependency Checks:**
  1. **PostgreSQL:** Executes `pool.Ping(ctx)` with a strict 3-second bounded timeout (`ReadinessTimeout`).
  2. **Redis:** Executes `redis.Ping(ctx)` with a strict 3-second bounded timeout (if Redis is configured).
- **Failure Behavior:**
  - If any dependency check fails or times out, the endpoint returns HTTP `503 Service Unavailable`.
  - The response adheres strictly to the canonical `ErrorResponse` envelope.
  - **Zero Leakage:** The response contains generic, safe dependency details and never exposes connection strings, internal IP addresses, stack traces, or raw SQL driver errors.
- **Success Response (HTTP 200):**
  ```json
  {
    "status": "ready",
    "dependencies": {
      "postgresql": "ok",
      "redis": "ok"
    }
  }
  ```
- **Failure Response (HTTP 503):**
  ```json
  {
    "error": {
      "code": "SERVICE_UNAVAILABLE",
      "message": "Database connection failed",
      "field": null,
      "details": {
        "dependency": "postgresql"
      }
    },
    "request_id": "req_1728593100123456789"
  }
  ```

---

## 5. Lifecycle Management & Graceful Termination

The platform relies on the centralized `lifecycle.Manager` (`backend/internal/platform/lifecycle/manager.go`) to coordinate startup and shutdown across subsystems.

### 5.1 Shutdown Sequence
When `SIGTERM` or `SIGINT` is received:
1. **Signal Interception:** The lifecycle signal watcher initiates context cancellation on the root lifecycle context.
2. **HTTP Traffic Drain:**
   - The HTTP server stops accepting new incoming TCP connections.
   - Active in-flight requests are granted up to `HTTPShutdownTimeout` (default 10s) to complete.
   - The rate limiter cleanup tickers are stopped.
3. **Worker & Poller Drain:**
   - The outbox poller loop detects context cancellation.
   - In-progress event dispatches finish within their allocated timeout, or are released back to `PENDING`.
   - The worker marks its status gauge `worker_active` as 0.
4. **Dependency Termination:**
   - Cleanup hooks execute with isolated per-hook timeouts (`CleanupItemTimeout`, default 10s) preventing hung hooks from blocking shutdown.
   - Database connection pools (`pgxpool.Pool`) and Redis clients are closed in reverse order of initialization.
5. **Timeout Bounding:** If any component exceeds its shutdown budget, shutdown completes with an error log to guarantee process termination.

---

## 6. Rate Limiting, Client IP Extraction, and Proxy Hardening

### 6.1 WS3 Hardening Resolutions
1. **Trusted Proxy CIDR Verification (WS3-01):**
   - Client IP extraction parses `X-Forwarded-For` using right-to-left traversal against a strictly configured `TrustedProxies` CIDR whitelist (`NEXORA_TRUSTED_PROXIES`).
   - Untrusted hops or forged spoofed client IPs are rejected; the furthest non-trusted upstream IP is selected as the authentic client identity.
2. **Distributed & Fallback Rate Limiting (WS3-03):**
   - The platform provides both `MemoryRateLimiter` and distributed `RedisRateLimiter`.
   - `RedisRateLimiter` enforces atomic sliding-window rate limiting across all cluster replicas via an atomic Lua script (`zremrangebyscore` -> `zcard` -> `zadd` -> `expire`).
   - If Redis is unavailable, the limiter fails gracefully according to policy (`fail-open`, `fail-closed`, or seamless fallback to node-local memory limiter).

---

## 7. Recommended Monitoring & Alerting Signals

Operational teams should establish Prometheus alert rules based on the following SLIs:

| Alert | Condition | Severity | Action |
| :--- | :--- | :--- | :--- |
| `HighHTTP5xxRate` | `rate(http_requests_total{status_class="5xx"}[5m]) / rate(http_requests_total[5m]) > 0.01` | Critical | Inspect API logs for unhandled errors or dependency timeouts. |
| `HighHTTPLatencyP99` | `histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m])) > 1.0` | Warning | Check PostgreSQL connection pool saturation and query performance. |
| `ServiceNotReady` | `probe_success{job="nexora-readyz"} == 0` for > 1m | Critical | Inspect database and Redis health; pod will be removed from load balancer rotation. |
| `OutboxDeadLetterSpike` | `rate(worker_dispatch_total{status="dead_letter"}[5m]) > 0` | High | Inspect outbox dead-letter events in `outbox_event` table; investigate downstream delivery failures. |
| `OutboxRetryAccumulation` | `rate(worker_dispatch_total{status="retry"}[5m]) > 5` | Warning | Check downstream event consumer latency or intermittent connectivity. |
| `DBPoolExhaustion` | `db_pool_acquired_conns / db_pool_total_conns > 0.9` for > 2m | Warning | Consider tuning `NEXORA_DB_MAX_CONNS` or investigating connection leaks. |
| `WorkerDown` | `worker_active == 0` on worker pods | Critical | Inspect worker logs for fatal startup failure or crash loop. |

---

## 8. Summary of Operational Capabilities & Architectural Boundaries

1. **Per-Instance Prometheus Scraping:** Handled via `/metrics` endpoint with normalized bounded cardinality; scraped per node using Kubernetes pod discovery.
2. **Distributed Tracing & OTLP/HTTP Export:** Implemented via W3C `traceparent` propagation and lightweight asynchronous `TraceExporter` (`backend/internal/platform/observability/exporter.go`) exporting OTLP/HTTP JSON spans with bounded worker queues and clean shutdown flush.
3. **Transactional Outbox Decoupling:** Outbox event claiming uses persistent database leases with `FOR UPDATE SKIP LOCKED`, decoupling external HTTP dispatch outside database transactions and preventing connection pool starvation.
4. **Child Audit Table RLS & Immutability:** Fully enforced at the PostgreSQL engine level across `audit.data_change`, `audit.security_event`, and `audit.login_history` with database-level immutability triggers.

---

## 9. OpenTelemetry OTLP/HTTP Protocol Specification & Interoperability

### 9.1 Wire Format & Endpoint Compatibility
The platform provides a zero-dependency OpenTelemetry trace exporter (`backend/internal/platform/observability/exporter.go`) adhering to the official OpenTelemetry OTLP/HTTP JSON specification:
- **Transport:** HTTP POST
- **Default Endpoint:** `http://127.0.0.1:4318/v1/traces` (configured via `NEXORA_OTLP_ENDPOINT`)
- **Content-Type:** `application/json`
- **Wire Payload Structure:**
  - `resourceSpans`: List of resource span envelopes containing `resource.attributes` (`service.name`, `service.version`, `deployment.environment`, and custom key-value pairs).
  - `scopeSpans`: List of instrumentation scopes containing `scope.name` (`example.com/nexora`) and `scope.version`.
  - `spans`: List of span objects with:
    - `traceId`: 32-hex string (16-byte W3C trace ID)
    - `spanId`: 16-hex string (8-byte span ID)
    - `parentSpanId`: 16-hex string (optional parent span ID)
    - `name`: Operation name (e.g. `HTTP GET /api/v1/healthz`)
    - `kind`: Integer enum (1 = INTERNAL, 2 = SERVER, 3 = CLIENT)
    - `startTimeUnixNano`: String-encoded 64-bit nanosecond timestamp (e.g. `"1760157600000000000"` as required by OTLP JSON spec)
    - `endTimeUnixNano`: String-encoded 64-bit nanosecond timestamp
    - `attributes`: Key-value pairs with `{"stringValue": "..."}`
    - `status`: `{"code": 1}` (STATUS_CODE_OK) or `{"code": 2, "message": "..."}` (STATUS_CODE_ERROR)

### 9.2 Interoperability Boundaries & Limitations
1. **JSON Encoding:** The exporter communicates exclusively via OTLP/HTTP JSON (`application/json`). Binary Protobuf over gRPC/HTTP (`application/x-protobuf`) is deliberately omitted to prevent pulling large external dependencies into `go.mod`. Any standard OpenTelemetry Collector or Jaeger instance configured with an OTLP/HTTP receiver (port 4318) accepts this format natively.
2. **Attribute Values:** Attributes are typed as `stringValue`. Numerical or boolean span attributes are cast to string values before serialization.
3. **Queue Bounding & Loss Policy:** Non-blocking bounded in-memory buffer (`NEXORA_OTLP_QUEUE_SIZE`, default 1000). Spans arriving while the queue is saturated are dropped and recorded in metric/log telemetry, ensuring business operations are never delayed by collector latency or failure.

---

## 10. Outbox Consumer Deduplication Boundary

### 10.1 Delivery Semantics
The transactional outbox engine guarantees **at-least-once delivery** to external dispatchers:
- Events are inserted atomically in the same database transaction as the business mutation.
- Background pollers claim batches using short transactions with `FOR UPDATE SKIP LOCKED` and persistent database leases (`claimed_by`, `lease_expires_at`).
- Dispatch handlers execute outside the database transaction.
- If a worker crashes after dispatch succeeds but before the finalization transaction commits, the lease will expire and the event will be redelivered upon restart.

### 10.2 Deduplication Contract for Downstream Consumers
1. **In-Memory Fixture Limitation:** The platform test suite includes `DeduplicatingConsumer` to verify that duplicate dispatches can be ignored by consumers. However, this implementation is in-memory and **does not survive consumer process restarts**.
2. **Durable Idempotency Requirement for Business Domains:** Downstream domain event consumers (in Phase 2 and external services) must implement **durable database-backed deduplication** wherever duplicate side effects cannot be tolerated. Consumers should record the event's unique UUID (`evt.ID`) in a persistent deduplication ledger (e.g. `processed_events (event_id UUID PRIMARY KEY, processed_at TIMESTAMPTZ)`) within their local transaction before executing business effects.

