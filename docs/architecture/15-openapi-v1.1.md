# POS OpenAPI v1.1 — Offline & Enterprise Contract

## 1. Purpose

OpenAPI v1.1 extends POS API v1 with the first-class offline POS contract.

Primary goals:

- preserve backward compatibility with OpenAPI v1;
- formalize device registration;
- formalize offline policy and quota;
- formalize durable sync submission;
- formalize reconciliation;
- formalize master-data synchronization;
- formalize terminal heartbeat;
- standardize offline error codes;
- preserve Idempotency-Key semantics;
- expose explicit conflict results;
- preserve server-authoritative business rules;
- provide machine-readable contracts for frontend and backend generation.

This document is a contract specification and is intended to be represented as an OpenAPI 3.x document in the implementation repository.

---

# 2. Compatibility Rule

OpenAPI v1.1 is an additive extension of v1.

Existing:

```text
/api/v1/*
```

resources remain valid unless explicitly deprecated in a future version.

New offline capabilities use:

```text
/api/v1/pos/*
```

No existing field is removed or semantically changed merely to support offline mode.

Breaking changes require a future API version or explicit migration strategy.

---

# 3. Global API Rules

## 3.1 Base Path

```text
/api/v1
```

## 3.2 Content Type

Request:

```http
Content-Type: application/json
```

Response:

```http
Content-Type: application/json
```

## 3.3 Authentication

```http
Authorization: Bearer <token>
```

Authentication mechanism may be JWT or another implementation-specific bearer mechanism.

The API contract does not expose token implementation details.

---

# 4. Canonical Headers

## 4.1 Request ID

```http
X-Request-ID: req_xxx
```

Optional client-provided correlation identifier.

Server must generate one if absent.

The response must include:

```http
X-Request-ID
```

or an equivalent documented request identifier.

---

# 5. Idempotency-Key

For idempotent critical commands:

```http
Idempotency-Key: <opaque-key>
```

Canonical header.

`X-Idempotency-Key` may only be supported as a compatibility alias.

If both are supplied:

```text
Idempotency-Key wins
```

or the request is rejected if values differ.

---

# 6. Idempotency Contract

Logical key:

```text
tenant_id
+
operation
+
idempotency_key
```

Server also stores a request hash.

Rules:

### Same key + same request

Return the original business result.

### Same key + different request

Return:

```text
409 IDEMPOTENCY_KEY_REUSED
```

### Network timeout

Client retries with the same key.

### Payment timeout

Client must not automatically create a new payment command with another key.

---

# 7. Conditional Request

For optimistic concurrency where supported:

```http
If-Match: "<version>"
```

If the version no longer matches:

```text
409 VERSION_CONFLICT
```

---

# 8. Tenant Context

Client tenant context is never trusted as authorization.

A header such as:

```http
X-Tenant-ID
```

may exist for already-authorized multi-tenant users but does not grant access.

For POS device operations, authoritative context is derived from:

```text
authenticated principal
+
device registration
+
terminal registration
+
server-side scope
```

---

# 9. Error Envelope

All business errors use:

```json
{
  "error": {
    "code": "STOCK_CONFLICT",
    "message": "Requested quantity is no longer available",
    "field": null,
    "details": {}
  },
  "request_id": "req_123"
}
```

Required:

```text
error.code
error.message
request_id
```

Optional:

```text
error.field
error.details
```

---

# 10. HTTP Status Mapping

| HTTP | Meaning |
|---|---|
| 200 | Success |
| 201 | Resource created |
| 202 | Accepted for async processing |
| 204 | Success without body |
| 400 | Malformed request |
| 401 | Authentication required/invalid |
| 403 | Permission/scope denied |
| 404 | Resource not found |
| 409 | Business/state/idempotency conflict |
| 422 | Semantic validation error |
| 429 | Rate limited |
| 500 | Internal error |
| 502 | Upstream/provider failure |
| 503 | Temporary unavailable |
| 504 | Upstream timeout |

---

# 11. Standard Error Codes

Core:

```text
VALIDATION_ERROR
UNAUTHENTICATED
PERMISSION_DENIED
SCOPE_DENIED
RESOURCE_NOT_FOUND
VERSION_CONFLICT
STATE_INVALID
IDEMPOTENCY_KEY_REUSED
RATE_LIMITED
INTERNAL_ERROR
SERVICE_UNAVAILABLE
```

Offline:

```text
DEVICE_NOT_REGISTERED
DEVICE_REVOKED
DEVICE_SUSPENDED
TERMINAL_DISABLED
OFFLINE_NOT_ALLOWED
OFFLINE_POLICY_EXPIRED
OFFLINE_QUOTA_EXCEEDED
CLOCK_DRIFT_EXCEEDED
PAYLOAD_VERSION_UNSUPPORTED
OFFLINE_OPERATION_NOT_ALLOWED
```

Sync:

```text
SYNC_ACCEPTED
SYNC_ALREADY_PROCESSED
SYNC_RETRYABLE
SYNC_PERMANENT_FAILURE
SYNC_CONFLICT
SYNC_RECONCILIATION_REQUIRED
```

Business conflicts:

```text
STOCK_CONFLICT
PRICE_VERSION_CONFLICT
PROMOTION_VERSION_CONFLICT
SESSION_STATE_CONFLICT
CUSTOMER_VERSION_CONFLICT
DOCUMENT_CONFLICT
PERMISSION_REVOKED
```

Payment:

```text
PAYMENT_UNKNOWN
PAYMENT_PROVIDER_TIMEOUT
PAYMENT_PROVIDER_REJECTED
PAYMENT_RECONCILIATION_REQUIRED
```

---

# 12. Device Registration

## Endpoint

```http
POST /api/v1/pos/devices/register
```

Permission:

```text
pos.device.register
```

Typical actor:

```text
System Administrator
Store Manager
Authorized Device Provisioning Service
```

Request:

```json
{
  "installation_id": "installation-uuid",
  "terminal_id": "terminal-uuid",
  "device_name": "POS Front Counter 01",
  "device_metadata": {
    "platform": "web",
    "browser": "Chrome",
    "app_version": "1.0.0"
  }
}
```

Server validates:

- terminal exists;
- terminal belongs to authorized store;
- terminal status;
- installation identity;
- provisioning policy;
- actor permission.

Response:

```json
{
  "device_id": "device-uuid",
  "installation_id": "installation-uuid",
  "tenant_id": "tenant-uuid",
  "company_id": "company-uuid",
  "store_id": "store-uuid",
  "terminal_id": "terminal-uuid",
  "status": "ACTIVE",
  "policy_version": "offline-policy-v1",
  "server_time": "2026-10-06T02:00:00Z"
}
```

---

# 13. Device Status

Possible:

```text
PENDING
ACTIVE
SUSPENDED
REVOKED
```

A revoked device cannot create new offline financial commands.

---

# 14. Offline Policy

## Endpoint

```http
GET /api/v1/pos/offline-policy
```

Authentication required.

Server derives context from authenticated device/session.

Response:

```json
{
  "version": "offline-policy-v1",
  "enabled": true,
  "effective_from": "2026-10-01T00:00:00Z",
  "expires_at": "2026-10-07T00:00:00Z",
  "allowed_operations": [
    "CREATE_OFFLINE_CASH_SALE"
  ],
  "limits": {
    "max_transaction_count": 50,
    "max_transaction_amount": 50000000,
    "max_single_transaction_amount": 5000000
  },
  "features": {
    "offline_cash_sale": true,
    "offline_card_payment": false,
    "offline_refund": false,
    "offline_voucher_redeem": false,
    "offline_loyalty_redeem": false,
    "offline_stock_adjustment": false
  },
  "clock_policy": {
    "max_drift_ms": 300000
  },
  "server_time": "2026-10-06T02:00:00Z"
}
```

---

# 15. Offline Quota

## Endpoint

```http
GET /api/v1/pos/offline-quota
```

Response:

```json
{
  "policy_version": "offline-policy-v1",
  "transaction_count": {
    "limit": 50,
    "used": 12,
    "remaining": 38
  },
  "amount": {
    "limit": 50000000,
    "used": 8500000,
    "remaining": 41500000,
    "currency": "IDR"
  },
  "server_time": "2026-10-06T02:00:00Z"
}
```

Client values are advisory.

Server performs final quota validation.

---

# 16. Master Data Sync

## Endpoint

```http
GET /api/v1/pos/sync/master-data
```

Query parameters:

```text
cursor
dataset
limit
```

Dataset:

```text
products
barcodes
prices
promotions
customers
taxes
configuration
```

Response:

```json
{
  "dataset": "products",
  "cursor": "next-cursor",
  "server_time": "2026-10-06T02:00:00Z",
  "version": "2026-10-06T02:00:00Z",
  "changes": [],
  "deleted_ids": []
}
```

---

# 17. Master Data Rules

The client may cache:

- products;
- barcodes;
- valid prices;
- explicitly offline-safe promotions;
- limited customer lookup data.

The client must not assume cached data remains authoritative.

Server validates all sensitive business state during command processing.

---

# 18. Sync Endpoint

## Endpoint

```http
POST /api/v1/pos/sync
```

Purpose:

Submit one or more offline commands.

Authentication required.

Device authentication/context required.

---

# 19. Sync Batch Request

```json
{
  "device_id": "device-uuid",
  "terminal_id": "terminal-uuid",
  "payload_version": 1,
  "commands": [
    {
      "local_command_id": "local-001",
      "client_transaction_id": "ct-001",
      "idempotency_key": "idem-001",
      "operation": "CREATE_OFFLINE_CASH_SALE",
      "client_sequence": 1001,
      "dependency_command_id": null,
      "created_at_device": "2026-10-06T09:00:00+07:00",
      "client_timezone": "Asia/Jakarta",
      "payload": {}
    }
  ]
}
```

Recommended default batch:

```text
20
```

Maximum:

```text
100
```

---

# 20. Sync Batch Rules

Server must:

1. authenticate;
2. validate device;
3. validate terminal;
4. validate scope;
5. validate offline policy;
6. validate payload version;
7. validate idempotency;
8. apply domain command;
9. create audit;
10. create outbox event;
11. persist idempotency result;
12. commit.

---

# 21. Sync Response

```json
{
  "request_id": "req-123",
  "server_time": "2026-10-06T02:01:00Z",
  "results": [
    {
      "local_command_id": "local-001",
      "client_transaction_id": "ct-001",
      "status": "SYNCED",
      "server_transaction_id": "sale-123",
      "server_document_id": "SO-20261006-000123",
      "result": {}
    }
  ]
}
```

---

# 22. Sync Result States

```text
SYNCED
FAILED_RETRYABLE
FAILED_PERMANENT
CONFLICT
RECONCILIATION_REQUIRED
```

---

# 23. Retryable Sync Response

Example:

```json
{
  "local_command_id": "local-001",
  "client_transaction_id": "ct-001",
  "status": "FAILED_RETRYABLE",
  "error": {
    "code": "SERVICE_UNAVAILABLE",
    "message": "Service temporarily unavailable"
  }
}
```

Client keeps the same:

```text
idempotency_key
client_transaction_id
```

---

# 24. Permanent Failure

Example:

```json
{
  "local_command_id": "local-001",
  "client_transaction_id": "ct-001",
  "status": "FAILED_PERMANENT",
  "error": {
    "code": "PRICE_VERSION_CONFLICT",
    "message": "Cached price is no longer valid"
  }
}
```

The command must not be blindly retried.

---

# 25. Conflict Response

Example:

```json
{
  "local_command_id": "local-001",
  "client_transaction_id": "ct-001",
  "status": "CONFLICT",
  "error": {
    "code": "STOCK_CONFLICT",
    "message": "Requested quantity is no longer available",
    "details": {
      "requested_qty": 5,
      "available_qty": 2,
      "variant_id": "variant-001"
    }
  }
}
```

---

# 26. Reconciliation Required

Example:

```json
{
  "local_command_id": "local-001",
  "client_transaction_id": "ct-001",
  "status": "RECONCILIATION_REQUIRED",
  "error": {
    "code": "PAYMENT_UNKNOWN",
    "message": "Payment result requires reconciliation"
  },
  "reconciliation_reference": "recon-001"
}
```

---

# 27. Reconciliation Endpoint

## Endpoint

```http
GET /api/v1/pos/sync/reconciliation/{client_transaction_id}
```

Response:

```json
{
  "client_transaction_id": "ct-001",
  "status": "CONFIRMED",
  "server_transaction_id": "sale-123",
  "server_document_id": "SO-20261006-000123",
  "payment_status": "SUCCESS",
  "inventory_status": "POSTED",
  "resolved_at": "2026-10-06T02:10:00Z"
}
```

---

# 28. Reconciliation States

```text
PENDING
CONFIRMED
FAILED
CONFLICT
REQUIRES_ACTION
```

---

# 29. Payment Reconciliation

Payment ambiguity must use a separate reconciliation path.

Example:

```text
POS
 |
 | charge
 v
Provider
 |
 X timeout
 |
 v
POS PAYMENT_UNKNOWN
 |
 v
Server reconciliation
 |
 +--> SUCCESS
 +--> FAILED
 +--> UNKNOWN
```

The client must never blindly submit another charge.

---

# 30. Terminal Heartbeat

## Endpoint

```http
POST /api/v1/pos/terminals/{terminal_id}/heartbeat
```

Purpose:

- terminal liveness;
- device health;
- policy refresh;
- server clock synchronization;
- operational telemetry.

Request:

```json
{
  "device_id": "device-uuid",
  "app_version": "1.0.0",
  "schema_version": "1",
  "queue_depth": 4,
  "oldest_queue_age_seconds": 120,
  "last_master_sync_at": "2026-10-06T01:58:00Z"
}
```

Response:

```json
{
  "server_time": "2026-10-06T02:01:00Z",
  "terminal_status": "ACTIVE",
  "device_status": "ACTIVE",
  "offline_policy_version": "offline-policy-v1",
  "policy_changed": false
}
```

---

# 31. Device Authorization

Every offline sync request must validate:

```text
device
+
installation
+
terminal
+
store
+
tenant
```

The server must reject mismatches.

Examples:

```text
DEVICE_TERMINAL_MISMATCH
DEVICE_STORE_MISMATCH
DEVICE_TENANT_MISMATCH
```

---

# 32. Permission Enforcement

Offline sync does not bypass normal permissions.

Example:

```text
sales.create
sales.confirm
payment.process
```

must still be evaluated.

If permission was revoked while offline:

```text
PERMISSION_REVOKED
```

Server decides whether the queued command can proceed based on the defined authorization policy.

---

# 33. Scope Enforcement

Permission must be evaluated together with:

```text
tenant
company
region
store
warehouse
terminal
```

Example:

```text
sales.void = true
```

does not mean:

```text
void every sale
```

The user must also have the required scope.

---

# 34. State Machine Enforcement

The API must not allow:

```http
PATCH /sales/orders/{id}
{
  "status": "COMPLETED"
}
```

for arbitrary status mutation.

Use commands:

```http
POST /sales/orders/{id}/confirm
POST /sales/orders/{id}/cancel
POST /sales/orders/{id}/void
```

Offline sync must map to equivalent domain commands.

---

# 35. Offline Command Operation Codes

Initial catalog:

```text
CREATE_OFFLINE_CASH_SALE
CREATE_OFFLINE_CASH_MOVEMENT
OPEN_OFFLINE_POS_SESSION
CLOSE_OFFLINE_POS_SESSION
```

Future controlled operations may be added through versioned contract changes.

Default:

```text
payment gateway
refund
voucher redemption
loyalty redemption
stock adjustment
promotion activation
```

are not offline-enabled.

---

# 36. Offline Cash Sale

Operation:

```text
CREATE_OFFLINE_CASH_SALE
```

Allowed only when:

```text
offline_enabled = true
policy allows operation
device active
terminal active
user authorized
quota available
clock within policy
```

---

# 37. Offline Session

Session operations require explicit policy.

Recommended:

```text
open online
operate offline
close with synchronization/reconciliation
```

The server remains authoritative for session state.

---

# 38. API Versioning

Current:

```text
v1
```

OpenAPI v1.1 is a contract revision, not:

```text
/api/v1.1
```

The API path remains:

```text
/api/v1
```

Contract version is managed through the OpenAPI specification version and backward-compatible schema evolution.

---

# 39. Schema Versioning

Offline payload:

```json
{
  "payload_version": 1
}
```

Event version is separate:

```text
event_version
```

API contract version is separate:

```text
OpenAPI v1.1
```

These must not be conflated.

---

# 40. Security Headers

Recommended:

```http
Authorization
X-Request-ID
Idempotency-Key
If-Match
```

Optional device correlation:

```http
X-Device-ID
```

However, `X-Device-ID` is contextual metadata and must not override authenticated device identity.

---

# 41. Rate Limiting

Sync endpoint must have rate limits.

Limits should consider:

```text
tenant
device
terminal
IP
authenticated principal
```

A burst of retry traffic must not overload the server.

---

# 42. Batch Size

Default:

```text
20 commands
```

Maximum:

```text
100 commands
```

The server may return:

```text
413 PAYLOAD_TOO_LARGE
```

if limits are exceeded.

---

# 43. Partial Batch Processing

A sync batch must support per-command result.

Example:

```text
C001 SYNCED
C002 CONFLICT
C003 SYNCED
C004 RETRYABLE
```

The entire batch must not be considered failed merely because one command failed.

Dependent commands must respect dependency relationships.

---

# 44. Transaction Boundary

Each authoritative business command should have its own domain transaction unless a specific atomic batch operation is explicitly defined.

Within the transaction:

```text
authorization context
RLS context
idempotency
domain command
state mutation
audit
outbox
idempotency result
```

Then:

```text
COMMIT
```

---

# 45. External Provider Calls

Never hold a long database transaction while waiting for:

```text
payment provider
ERP
external HTTP API
```

Use:

```text
core transaction
→ outbox
→ worker
→ external provider
→ result callback/event
```

---

# 46. OpenAPI Security Schemes

Conceptually:

```yaml
securitySchemes:
  bearerAuth:
    type: http
    scheme: bearer
```

Endpoint-specific authorization remains application-level.

---

# 47. Reusable Schemas

OpenAPI v1.1 should define reusable schemas for:

```text
Error
ErrorDetails
RequestMetadata
Pagination
DeviceContext
OfflinePolicy
OfflineQuota
SyncCommand
SyncResult
SyncBatchRequest
SyncBatchResponse
Conflict
Reconciliation
MasterDataSyncResponse
HeartbeatRequest
HeartbeatResponse
```

---

# 48. SyncCommand Schema

Conceptual:

```yaml
SyncCommand:
  type: object
  required:
    - local_command_id
    - client_transaction_id
    - idempotency_key
    - operation
    - payload_version
    - client_sequence
    - payload
```

---

# 49. Error Details

`details` is structured and machine-readable.

Example:

```json
{
  "error": {
    "code": "STOCK_CONFLICT",
    "message": "Stock conflict",
    "field": "items[0].qty",
    "details": {
      "variant_id": "v1",
      "requested": 5,
      "available": 2
    }
  },
  "request_id": "req-123"
}
```

Do not put secrets or full payment credentials into `details`.

---

# 50. Pagination

Existing APIs should continue using the v1 pagination contract.

Offline master-data sync may use cursor-based pagination:

```text
cursor
```

because master-data synchronization requires deterministic continuation.

---

# 51. Master Sync Consistency

A master-data response should include:

```text
cursor
server_time
version
changes
deleted_ids
```

The client commits the new cursor only after successfully persisting the received changes.

---

# 52. Cursor Safety

Never advance:

```text
cursor
```

before:

```text
changes persisted successfully
```

Otherwise data can be skipped after crash.

---

# 53. Offline Policy Refresh

Policy should be refreshed through:

```text
heartbeat
offline-policy
sync response
```

If policy changes:

```text
policy_changed = true
```

client refreshes policy.

---

# 54. Policy Expiry

If:

```text
offline_expires_at < current server-authorized time
```

new offline financial commands must be blocked.

Existing queued commands remain recoverable.

---

# 55. Clock Drift

Heartbeat response contains:

```text
server_time
```

Client calculates:

```text
clock_offset_ms
```

If drift exceeds policy:

```text
CLOCK_DRIFT_EXCEEDED
```

New offline financial commands may be blocked.

---

# 56. API Audit

Every critical command must produce server-side audit.

Examples:

```text
SALE_CREATED
SALE_COMPLETED
SALE_VOIDED
PAYMENT_CREATED
PAYMENT_SUCCESS
STOCK_ADJUSTED
POS_SESSION_OPENED
POS_SESSION_CLOSED
```

Offline origin must be represented in metadata:

```text
origin = OFFLINE_SYNC
```

---

# 57. Event Integration

Successful commands produce domain events through transactional outbox.

Example:

```text
CREATE_OFFLINE_CASH_SALE
        |
        +--> SALE_CREATED
        +--> SALE_COMPLETED
        +--> PAYMENT_CREATED
        +--> PAYMENT_SUCCESS
        +--> STOCK_DEDUCTED
```

Exact events depend on the authoritative domain transaction.

---

# 58. Event Is Not API Response

The API response provides immediate command result.

The event is an asynchronous integration contract.

Do not force clients to consume domain events to determine whether their synchronous command succeeded.

---

# 59. OpenAPI and Event Versioning

OpenAPI version:

```text
v1.1
```

Event version:

```text
event_version = 1
```

Payload version:

```text
payload_version = 1
```

They evolve independently.

---

# 60. Deprecation

Deprecated endpoints must:

- remain functional during defined migration period;
- be documented;
- emit appropriate deprecation headers where implemented;
- have replacement documented;
- not silently change semantics.

---

# 61. Generated Clients

OpenAPI v1.1 should be used to generate:

- TypeScript API types/client;
- Go API types where practical;
- contract validation.

Generated code must not become the domain model.

---

# 62. Validation

CI must validate:

```text
OpenAPI syntax
OpenAPI schema
duplicate operation IDs
schema references
request/response examples
security definitions
breaking changes
generated client compatibility
```

Recommended tools may include:

```text
Redocly
Spectral
openapi-diff
Swagger Parser
```

The implementation repository may choose the exact tooling.

---

# 63. Contract Testing

Minimum contract tests:

```text
device registration
offline policy
offline quota
master sync
sync success
sync retryable
sync permanent
sync conflict
payment reconciliation
heartbeat
error envelope
idempotency reuse
permission denied
scope denied
```

---

# 64. API Acceptance Criteria

OpenAPI v1.1 is accepted only if:

1. all v1 endpoints remain compatible;
2. offline endpoints are documented;
3. device registration is documented;
4. policy is documented;
5. quota is documented;
6. sync batch is documented;
7. reconciliation is documented;
8. master-data sync is documented;
9. heartbeat is documented;
10. Idempotency-Key semantics are explicit;
11. error codes are machine-readable;
12. conflict response is machine-readable;
13. payment ambiguity is explicit;
14. state transitions remain command-oriented;
15. permission and scope requirements are documented;
16. server remains authoritative;
17. OpenAPI validates in CI;
18. examples validate against schemas;
19. generated clients compile;
20. backward compatibility is checked automatically.

---

# 65. Architecture Locks

### LOCK-OAPI-001

API base path remains:

```text
/api/v1
```

### LOCK-OAPI-002

OpenAPI v1.1 is additive.

### LOCK-OAPI-003

`Idempotency-Key` is canonical.

### LOCK-OAPI-004

Offline sync uses stable idempotency keys across retries.

### LOCK-OAPI-005

Client tenant/device/store fields are never authorization authority.

### LOCK-OAPI-006

Critical state changes use commands.

### LOCK-OAPI-007

Offline sync returns per-command outcomes.

### LOCK-OAPI-008

Payment ambiguity is represented explicitly.

### LOCK-OAPI-009

Reconciliation is first-class.

### LOCK-OAPI-010

Master-data synchronization uses cursors.

### LOCK-OAPI-011

Cursor advances only after local persistence succeeds.

### LOCK-OAPI-012

Offline policy and quota are server-controlled.

### LOCK-OAPI-013

OpenAPI contract changes require CI validation.

### LOCK-OAPI-014

Breaking API changes require explicit versioning/migration.

### LOCK-OAPI-015

Domain events are not synchronous API responses.

---

# 66. Final API Architecture

```text
                         ┌─────────────────────┐
                         │      POS Client     │
                         └──────────┬──────────┘
                                    │
                              HTTPS / JSON
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │      API Gateway    │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │     Go API Layer    │
                         │                     │
                         │ Auth                │
                         │ Scope               │
                         │ Idempotency         │
                         │ Validation          │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │ Application Command │
                         │       Layer         │
                         └──────────┬──────────┘
                                    │
                                    ▼
                         ┌─────────────────────┐
                         │    Domain Layer     │
                         │                     │
                         │ State Machine       │
                         │ Business Rules      │
                         └──────────┬──────────┘
                                    │
                       ┌────────────┼────────────┐
                       ▼            ▼            ▼
                  PostgreSQL      Audit       Outbox
                       │                         │
                       │                         ▼
                       │                    Workers
                       │                         │
                       │             ┌───────────┼───────────┐
                       │             ▼           ▼           ▼
                       │           ERP        Payment     Reporting
                       │
                       └──── System of Record
```

---

# 67. Final Principle

OpenAPI v1.1 is intentionally a **contract layer**, not a business-rule layer.

The architecture remains:

```text
API
 ↓
Application Command
 ↓
Authorization
 ↓
Domain
 ↓
Persistence
 ↓
Audit + Outbox
 ↓
Commit
 ↓
Async Integration
```

This keeps the POS platform extensible toward ecommerce, dealer, mobile, marketplace, finance, ERP, and eventually Odoo-like capabilities without coupling the API directly to database tables or external providers.
