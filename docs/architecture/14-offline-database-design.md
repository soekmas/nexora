# POS Offline Database Design v1

## 1. Purpose

This document defines the local persistence architecture for the POS offline-capable platform.

The local database is a **durable operational cache and command queue**. It is not the system of record.

Primary goals:

- survive browser/application crash;
- preserve locally created operational work;
- support controlled offline sales;
- provide at-least-once delivery to the server;
- achieve exactly-once business effect through server-side idempotency;
- support explicit conflict and reconciliation;
- prevent payment ambiguity from becoming duplicate charges;
- separate device identity from user identity;
- enforce offline policy and quota;
- support FIFO ordering where operations are dependent;
- protect sensitive local data;
- allow deterministic cleanup and schema migration.

Non-goals:

- becoming an independent authoritative inventory database;
- authoritative payment state;
- authoritative loyalty/voucher state;
- bypassing server authorization;
- storing unrestricted financial history forever;
- silently resolving business conflicts.

---

## 2. Architecture Position

```text
POS UI
  |
  +--> Local Application State
  |
  +--> Local Database
  |       |
  |       +--> Master Cache
  |       +--> Cart / Workspace
  |       +--> Command Queue
  |       +--> Sync Attempts
  |       +--> Conflict / Reconciliation
  |       +--> Device / Session Metadata
  |
  +--> API Client
          |
          v
       POS API
          |
          v
       PostgreSQL
       SYSTEM OF RECORD
```

The local database must never be treated as final truth for:

- inventory;
- payment settlement;
- customer balance;
- loyalty balance;
- voucher validity;
- financial posting;
- ERP status;
- permissions;
- user role;
- terminal authorization;
- promotion activation;
- final document numbering.

---

## 3. Recommended Technology

### 3.1 Primary

Browser POS:

- IndexedDB;
- application-level repository;
- a small IndexedDB wrapper such as Dexie or equivalent may be used;
- schema migrations must be versioned.

The architecture does not depend on a specific IndexedDB wrapper.

### 3.2 Why IndexedDB

Requirements:

- durable browser storage;
- transactional writes;
- structured objects;
- asynchronous API;
- suitable for offline queue;
- significantly better than localStorage for structured POS workload.

`localStorage` must not be used as the command queue.

### 3.3 Optional Future Native POS

If POS later becomes a native/desktop application, the logical schema in this document remains authoritative at the domain level. The physical implementation may move to SQLite or another local embedded database without changing the sync contract.

---

## 4. Local Data Classification

| Data | Local? | Authority |
|---|---:|---|
| Product cache | Yes | Server |
| Barcode cache | Yes | Server |
| Price cache | Yes | Server |
| Offline-safe promotion cache | Yes | Server |
| Customer lookup cache | Yes, limited | Server |
| Cart | Yes | Local workspace |
| Offline sale command | Yes | Server after sync |
| Payment gateway result | Metadata only | Provider/server |
| Inventory | Cache only | Server |
| Stock reservation | Pending command only | Server |
| Loyalty balance | Cache only | Server |
| Voucher status | Cache only | Server |
| User permissions | Cached policy | Server |
| Device identity | Yes | Server-issued identity |
| Terminal policy | Cached | Server |
| Sync queue | Yes | Server processing |
| Audit metadata | Yes, pending metadata | Server |
| ERP status | No authoritative copy | ERP/integration |

---

# 5. Database Stores

The logical database contains the following stores:

```text
device_context
terminal_context
master_products
master_barcodes
master_prices
master_promotions
master_customers
cart_sessions
cart_items
offline_commands
sync_attempts
sync_conflicts
reconciliation_items
local_events
local_settings
schema_metadata
```

Optional stores:

```text
receipt_snapshots
printer_jobs
cash_drawer_jobs
```

These optional stores must not contain authoritative financial state.

---

# 6. Entity: device_context

Purpose:

Represent the installation/device identity.

Fields:

```text
device_id                 UUID
installation_id           UUID
tenant_id                 UUID
company_id                UUID
store_id                  UUID
terminal_id               UUID
status                    ACTIVE | SUSPENDED | REVOKED
device_public_key_ref     string nullable
registered_at             timestamp
last_server_sync_at       timestamp
last_server_time_at       timestamp
clock_offset_ms           integer
policy_version            string
schema_version            string
created_at                timestamp
updated_at                timestamp
```

Rules:

1. `device_id` is not the same as user ID.
2. `installation_id` identifies a specific installation.
3. Device identity is issued/registered by server.
4. A revoked device cannot create new offline financial commands.
5. Existing queued commands are not silently deleted after revocation.
6. Server decides whether queued commands may still be processed.

---

# 7. Entity: terminal_context

Purpose:

Cache terminal-specific operational policy.

Fields:

```text
terminal_id
store_id
terminal_status
offline_enabled
offline_policy_version
offline_started_at
offline_expires_at
max_offline_transaction_count
max_offline_transaction_amount
remaining_offline_transaction_count
remaining_offline_amount
last_policy_refresh_at
server_time_at_refresh
cached_permissions_version
```

The local copy is informational/enabling only.

Server revalidates policy during sync.

---

# 8. Entity: master_products

Purpose:

Offline product lookup.

Fields:

```text
product_id
variant_id
sku
product_name
brand_id
category_id
uom_id
status
tax_category
version
effective_from
effective_to
last_synced_at
```

Rules:

- immutable identifiers;
- product name may be refreshed;
- inactive products may remain cached for historical cart display;
- new offline sale must not assume a cached product is currently sellable;
- server validates product status at synchronization.

---

# 9. Entity: master_barcodes

Fields:

```text
barcode_id
product_id
variant_id
barcode
barcode_type
is_primary
status
version
last_synced_at
```

Indexes:

```text
barcode
variant_id
product_id
```

Barcode uniqueness must be resolved by server master data.

---

# 10. Entity: master_prices

Purpose:

Cache prices that may be used offline when explicitly allowed.

Fields:

```text
price_id
price_list_id
product_id
variant_id
currency
unit_price
min_qty
max_qty
effective_from
effective_to
version
offline_allowed
last_synced_at
```

Critical rule:

The client cannot extend or modify price validity.

If:

```text
current_time > effective_to
```

the client must not use the price as a valid offline price.

Server time remains authoritative.

---

# 11. Entity: master_promotions

Only promotions explicitly marked offline-safe may be cached for offline calculation.

Fields:

```text
promotion_id
promotion_version_id
promotion_code
priority
stacking_mode
conditions_snapshot
rewards_snapshot
effective_from
effective_to
offline_allowed
version
last_synced_at
```

The local promotion engine may calculate a provisional result.

Server recalculates/validates the final result during sync.

Promotion versions are immutable.

---

# 12. Entity: master_customers

Customer cache must be minimized.

Fields:

```text
customer_id
customer_code
display_name
customer_group_id
status
version
last_synced_at
```

Sensitive fields should not be cached unless necessary.

Examples normally excluded:

- password;
- payment credentials;
- full identity documents;
- full payment card data;
- unnecessary PII.

---

# 13. Entity: cart_sessions

Purpose:

Durable local shopping cart/workspace.

Fields:

```text
cart_id
terminal_id
device_id
user_id
status
currency
subtotal
discount_total
tax_total
grand_total
local_version
created_at
updated_at
```

Status:

```text
ACTIVE
CHECKOUT
CONVERTED
ABANDONED
EXPIRED
```

A cart is not a sale.

The server creates the authoritative sales order during command processing.

---

# 14. Entity: cart_items

Fields:

```text
cart_item_id
cart_id
product_id
variant_id
sku_snapshot
name_snapshot
qty
uom_id
unit_price_snapshot
discount_snapshot
tax_snapshot
line_total
promotion_snapshot
created_at
updated_at
```

Snapshots exist for UX and diagnostics.

They do not override server-side validation.

---

# 15. Entity: offline_commands

This is the most important local store.

Purpose:

Persist business commands that must eventually be submitted to the server.

Fields:

```text
local_command_id
client_transaction_id
idempotency_key
operation
payload_version

tenant_id
company_id
store_id
terminal_id
device_id
user_id

client_sequence
dependency_command_id

payload
payload_hash

created_at_device
client_timezone
created_at_local

state

attempt_count
next_attempt_at
last_attempt_at

server_request_id
server_transaction_id
server_document_id

created_at
updated_at
```

State:

```text
LOCAL_ONLY
QUEUED
SYNCING
SYNCED
FAILED_RETRYABLE
FAILED_PERMANENT
CONFLICT
RECONCILIATION_REQUIRED
CANCELLED
```

---

# 16. Command Identity

The local system must maintain several distinct identifiers.

### local_command_id

Unique local database record.

### client_transaction_id

Stable business transaction identity generated by client.

### idempotency_key

Stable request identity sent to server.

### server_request_id

Server-generated request/trace correlation.

### server_transaction_id

Authoritative server transaction identifier.

### server_document_id

Authoritative business document identifier.

They must not be conflated.

---

# 17. Idempotency Rules

The client must never generate a new idempotency key merely because a network request failed.

Example:

```text
Attempt 1
Idempotency-Key = ABC

timeout

Attempt 2
Idempotency-Key = ABC

timeout

Attempt 3
Idempotency-Key = ABC
```

The server determines whether the command already succeeded.

Never:

```text
Attempt 1 = ABC
Attempt 2 = XYZ
```

for the same business command.

---

# 18. Payload Hash

The client stores:

```text
payload_hash
```

The server independently calculates the request hash.

If the same idempotency key is reused with a different payload:

```text
IDEMPOTENCY_KEY_REUSED
```

The command becomes:

```text
FAILED_PERMANENT
```

or explicit conflict depending on server response.

---

# 19. FIFO Ordering

Commands that depend on each other must preserve ordering per terminal.

Example:

```text
C001 Open Session
C002 Create Sale
C003 Complete Sale
C004 Cash Movement
C005 Close Session
```

C003 cannot be processed before C002.

`client_sequence` provides deterministic ordering.

`dependency_command_id` explicitly represents hard dependency.

---

# 20. Independent Commands

Commands without dependency may be processed independently.

Example:

```text
C101 product cache refresh
C102 customer cache refresh
C103 receipt reprint metadata
```

However, synchronization must not sacrifice correctness for parallelism.

---

# 21. sync_attempts

Purpose:

Operational retry history.

Fields:

```text
attempt_id
local_command_id
attempt_number
started_at
completed_at
network_status
http_status
error_code
error_class
server_request_id
response_hash
duration_ms
created_at
```

Error class:

```text
NETWORK
TIMEOUT
AUTHENTICATION
AUTHORIZATION
VALIDATION
CONFLICT
SERVER_RETRYABLE
SERVER_PERMANENT
PAYMENT_AMBIGUOUS
UNKNOWN
```

Sensitive response payloads must not be stored unnecessarily.

---

# 22. sync_conflicts

Fields:

```text
conflict_id
local_command_id
client_transaction_id
conflict_code
server_state
local_state
server_version
local_version
details
resolution_status
resolution_action
resolved_by
resolved_at
created_at
```

Status:

```text
OPEN
IN_REVIEW
RESOLVED
CANCELLED
```

Conflict resolution must be explicit.

No silent overwrite.

---

# 23. reconciliation_items

Purpose:

Track commands requiring authoritative server reconciliation.

Fields:

```text
reconciliation_id
local_command_id
client_transaction_id
reconciliation_type
status
server_reference
provider_reference
last_checked_at
next_check_at
resolution
resolved_at
resolved_by
created_at
```

Types:

```text
PAYMENT_UNKNOWN
SERVER_RESULT_UNKNOWN
DUPLICATE_SUSPECTED
INVENTORY_CONFLICT
SESSION_CONFLICT
DOCUMENT_NUMBER_CONFLICT
```

---

# 24. Payment Ambiguity

Payment is the highest-risk offline condition.

Example:

```text
Client -> payment provider
          |
          +-- request sent
          |
          +-- network timeout
```

The client must NOT assume:

```text
FAILED
```

and must NOT blindly retry the payment.

Local state:

```text
RECONCILIATION_REQUIRED
```

Server/provider reconciliation determines the authoritative result.

---

# 25. local_events

This is not the same as server domain events.

Purpose:

Track local operational events needed for diagnostics.

Example:

```text
LOCAL_CART_CREATED
LOCAL_COMMAND_QUEUED
SYNC_STARTED
SYNC_FAILED
SYNC_SUCCEEDED
DEVICE_REVOKED
OFFLINE_POLICY_EXPIRED
CLOCK_DRIFT_DETECTED
```

Local events are not authoritative domain events.

They must never be published to ERP as if they were business events.

---

# 26. local_settings

Only non-sensitive configuration.

Examples:

```text
ui_preferences
printer_preferences
barcode_scanner_preferences
sync_batch_size
last_master_sync_cursor
```

Do not store secrets here.

---

# 27. schema_metadata

Fields:

```text
schema_version
migration_version
installed_at
last_migrated_at
database_instance_id
```

Migration must be forward-only and tested.

---

# 28. Offline Sale Command Payload

Example logical payload:

```json
{
  "client_transaction_id": "ct_123",
  "operation": "CREATE_OFFLINE_CASH_SALE",
  "payload_version": 1,
  "terminal_id": "terminal-01",
  "device_id": "device-01",
  "client_sequence": 1024,
  "customer_id": "customer-01",
  "items": [
    {
      "variant_id": "variant-01",
      "qty": 2,
      "unit_price": 150000
    }
  ],
  "payment": {
    "method": "CASH",
    "amount": 300000
  },
  "created_at_device": "2026-10-06T09:00:00+07:00"
}
```

The server remains authoritative for:

- product;
- price;
- tax;
- promotion;
- stock;
- customer;
- payment;
- numbering;
- permissions;
- terminal;
- offline policy.

---

# 29. Local Transaction Lifecycle

```text
ACTIVE CART
   |
   v
CHECKOUT
   |
   v
COMMAND CREATED
   |
   v
LOCAL_ONLY
   |
   v
QUEUED
   |
   v
SYNCING
   |
   +---- SUCCESS ----------------> SYNCED
   |
   +---- RETRYABLE -------------> FAILED_RETRYABLE
   |                                  |
   |                                  v
   |                                QUEUED
   |
   +---- CONFLICT --------------> CONFLICT
   |
   +---- PAYMENT UNKNOWN -------> RECONCILIATION_REQUIRED
   |
   +---- PERMANENT ERROR ------> FAILED_PERMANENT
```

---

# 30. Crash Recovery

The following sequence must be atomic at local database level:

```text
create command
+
persist payload
+
persist idempotency key
+
persist sequence
+
set QUEUED
```

If the browser crashes afterward, the command must remain recoverable.

The UI must never depend solely on in-memory state.

---

# 31. Sync Worker

Recommended flow:

```text
load eligible commands
        ↓
lock local command
        ↓
validate local prerequisites
        ↓
send Idempotency-Key
        ↓
receive response
        ↓
persist result atomically
        ↓
update command state
        ↓
persist sync attempt
        ↓
release lock
```

The client must assume network delivery is at-least-once.

---

# 32. Retry Policy

Recommended initial backoff:

```text
5 sec
15 sec
30 sec
60 sec
2 min
5 min
15 min
30 min
```

Add jitter.

Maximum attempts should be policy-based.

Permanent business errors must not be retried indefinitely.

---

# 33. Retryable Examples

```text
NETWORK_ERROR
TIMEOUT
HTTP 502
HTTP 503
HTTP 504
TEMPORARY_DATABASE_ERROR
SERVER_RETRYABLE
```

---

# 34. Permanent Examples

```text
INVALID_PAYLOAD
PAYLOAD_VERSION_UNSUPPORTED
TERMINAL_DISABLED
DEVICE_REVOKED
PERMISSION_REVOKED
OFFLINE_POLICY_VIOLATION
PRODUCT_NOT_FOUND
PRICE_EXPIRED
PROMOTION_INVALID
INVALID_STATE_TRANSITION
```

---

# 35. Conflict Examples

```text
STOCK_CONFLICT
PRICE_VERSION_CONFLICT
PROMOTION_VERSION_CONFLICT
SESSION_STATE_CONFLICT
CUSTOMER_VERSION_CONFLICT
DOCUMENT_CONFLICT
IDEMPOTENCY_KEY_REUSED
```

---

# 36. Offline Quota

The local system must track:

```text
transaction_count
transaction_amount
offline_start
offline_expiry
remaining_count
remaining_amount
```

Quota is server-defined.

Client cannot increase quota.

When quota is exhausted:

```text
new offline financial transaction = BLOCKED
```

Existing queued commands remain preserved.

---

# 37. Clock Handling

Local device time is diagnostic only.

Store:

```text
created_at_device
client_timezone
server_time_at_last_sync
clock_offset_ms
```

If clock drift exceeds policy:

```text
CLOCK_DRIFT_EXCEEDED
```

New offline financial commands may be blocked.

Existing queued commands must not be silently deleted.

---

# 38. Data Retention

Local data must have retention rules.

Example:

```text
SYNCED commands          7–30 days
FAILED commands           policy-based
Conflict records          until resolved + retention
Master cache              until replaced/expired
Cart drafts               policy-based
Sync attempts              7–30 days
Local diagnostics         short retention
```

Final retention values belong to production policy.

---

# 39. Local Storage Security

Never store:

```text
password
password hash
CVV
full PAN
payment provider secret
API secret
JWT private signing key
refresh token in plaintext where avoidable
MFA secret
encryption master key
```

If authentication/session tokens are required locally:

- minimize lifetime;
- use secure browser mechanisms where available;
- use refresh/re-authentication policy;
- revoke server-side;
- never treat local token storage as trusted authorization.

---

# 40. Sensitive Payload Encryption

Where supported, sensitive local fields should be encrypted.

However:

> Browser-side encryption does not make a compromised browser trusted.

Therefore:

```text
Encryption = defense in depth
Server authorization = authority
```

---

# 41. Multi-User Terminal

A terminal may have multiple cashier users.

The command must record:

```text
user_id
device_id
terminal_id
store_id
```

The server validates that the user was authorized to act on the terminal at the time of synchronization according to policy.

A cached user permission cannot override server revocation.

---

# 42. Device Revocation

When server returns:

```text
DEVICE_REVOKED
```

the client must:

1. stop creating new offline financial commands;
2. preserve queued commands;
3. mark device context revoked;
4. notify operator;
5. prevent destructive local cleanup;
6. attempt reconciliation according to server policy.

---

# 43. Offline Master Data Sync

Recommended cursor model:

```text
master_sync_cursor
```

Per dataset:

```text
products
barcodes
prices
promotions
customers
taxes
configuration
```

Server provides incremental changes.

Example:

```text
GET /api/v1/pos/sync/master-data?cursor=abc
```

Response:

```text
next_cursor
changes[]
deleted_ids[]
server_time
version
```

---

# 44. Tombstones

Master records deleted/deactivated on server may require tombstones locally.

Example:

```text
product_id
deleted = true
server_version
deleted_at
```

This prevents stale local records from being resurrected.

---

# 45. Local Database Transaction Boundaries

IndexedDB transactions should be small.

Example:

```text
Transaction A:
  create offline command
  update cart
  commit

Network call

Transaction B:
  update sync attempt
  update command state
  persist server result
  commit
```

Never hold a local database transaction open while waiting for network response.

---

# 46. Exactly-Once Business Effect

The client provides:

```text
at-least-once delivery
```

The server guarantees business idempotency:

```text
Idempotency-Key
+
request hash
+
authoritative transaction
```

Therefore:

```text
network delivery
    = at least once

business effect
    = exactly once
```

where the server successfully processes an idempotent command.

---

# 47. Offline Receipt

Receipt may be generated locally as:

```text
PENDING SERVER SYNC
```

It must not falsely imply:

- ERP posted;
- payment settled;
- inventory finalized;
- financial posting completed.

After successful synchronization, the receipt may be replaced/enriched with authoritative references.

---

# 48. Local Document Number

Offline client must not use:

```text
MAX(number) + 1
```

and should not invent authoritative invoice numbers.

Use:

```text
client_transaction_id
```

until server assigns authoritative business number.

---

# 49. Offline Inventory

The local database may show:

```text
cached stock
```

but must label it as cached/estimated.

It cannot guarantee:

```text
available_to_sell
```

unless the server policy explicitly allows the offline transaction.

Server resolves final stock.

---

# 50. Offline Promotion

Only:

```text
offline_allowed = true
```

promotion versions may be used.

The local result is provisional.

Server re-evaluates:

```text
eligibility
priority
stacking
reward
discount
tax
```

---

# 51. Offline Loyalty

Default:

```text
earn = deferred
redeem = disabled
```

The local database must not directly mutate loyalty balance.

---

# 52. Offline Voucher

Default:

```text
redeem = disabled
```

because voucher uniqueness and concurrent redemption require server authority.

---

# 53. Offline Refund

Default:

```text
disabled
```

Refund requires server-authoritative transaction/payment state.

---

# 54. Local Audit Metadata

For queued commands store:

```text
user_id
device_id
terminal_id
store_id
client_timestamp
client_sequence
operation
payload_hash
```

Server generates authoritative audit record after authorization and processing.

Local metadata is evidence/context, not final audit truth.

---

# 55. Conflict UX

The POS UI must not show:

```text
Something went wrong.
```

only.

It should provide actionable status:

```text
Transaction pending synchronization.
Stock changed before synchronization.
Payment result requires verification.
Terminal authorization changed.
Price is no longer valid.
```

Where possible, provide:

```text
Retry
Review
Reconcile
Cancel local draft
Contact supervisor
```

depending on conflict type.

---

# 56. Offline Database Index Strategy

Important indexes:

```text
offline_commands(state)
offline_commands(next_attempt_at)
offline_commands(client_sequence)
offline_commands(client_transaction_id)
offline_commands(idempotency_key)
offline_commands(dependency_command_id)

sync_attempts(local_command_id)
sync_conflicts(local_command_id)
reconciliation_items(status)
master_barcodes(barcode)
master_products(sku)
```

Composite indexes may be required for terminal-specific queues.

Example:

```text
(state, terminal_id, client_sequence)
```

---

# 57. Local Concurrency

Multiple browser tabs/windows for the same terminal must be prevented or controlled.

Recommended:

```text
single active POS runtime per terminal
```

Use:

- Web Locks API where available;
- BroadcastChannel;
- local heartbeat;
- server terminal session validation.

Do not assume browser tab locking alone is sufficient.

---

# 58. Queue Lease

Sync workers should use a local lease:

```text
lease_owner
lease_until
```

to prevent two workers processing the same command concurrently.

Lease expiration allows recovery after browser crash.

---

# 59. Queue Poisoning Protection

A permanently failing command must not block the entire queue forever.

Example:

```text
C001 = permanent error
C002 = valid
C003 = valid
```

The system may move C001 to:

```text
FAILED_PERMANENT
```

while preserving FIFO constraints for dependent commands.

Dependencies must still block dependent commands where required.

---

# 60. Reconciliation Rules

Reconciliation must be server-driven.

Example:

```text
GET /api/v1/pos/sync/reconciliation/{client_transaction_id}
```

Response can provide:

```text
PENDING
CONFIRMED
FAILED
PAYMENT_UNKNOWN
CONFLICT
REQUIRES_ACTION
```

The local client updates its projection accordingly.

---

# 61. Local Database Versioning

Example:

```text
DB v1
  ↓
DB v2
  ↓
DB v3
```

Every migration must:

- be deterministic;
- be backward-aware where necessary;
- preserve queued commands;
- preserve idempotency keys;
- preserve reconciliation records;
- be tested against realistic production data.

Never drop `offline_commands` during a routine migration.

---

# 62. Disaster / Browser Storage Loss

If IndexedDB is cleared:

- local-only carts may be lost;
- queued commands not synced may be lost;
- server-authoritative transactions remain safe;
- server reconciliation remains authoritative.

Therefore the UI should warn users before destructive browser reset/logout operations where queued work exists.

---

# 63. Logout Policy

If user logs out while commands exist:

```text
queued_commands > 0
```

the system must not silently delete them.

Possible policy:

```text
allow logout
retain commands
associate original user_id
require device re-authentication before sync if policy demands
```

Server decides whether the original command remains authorized.

---

# 64. Offline Command Security

The client must never be trusted for:

```text
tenant_id
company_id
store_id
terminal_id
user_id
price
discount
tax
promotion result
stock
payment result
permission
approval
```

These fields may exist in the local payload for context, but server validation is mandatory.

---

# 65. Server Sync Contract

Client sends:

```text
Idempotency-Key
client_transaction_id
operation
payload_version
device_id
terminal_id
client_sequence
payload
```

Server returns:

```text
request_id
processing_status
authoritative_result
server_transaction_id
server_document_id
conflicts
reconciliation_reference
```

---

# 66. Offline API Mapping

```text
POST /api/v1/pos/devices/register
GET  /api/v1/pos/offline-policy
GET  /api/v1/pos/offline-quota
POST /api/v1/pos/sync
GET  /api/v1/pos/sync/reconciliation/{client_transaction_id}
GET  /api/v1/pos/sync/master-data
POST /api/v1/pos/terminals/{terminal_id}/heartbeat
```

---

# 67. Observability

Client telemetry should include:

```text
device_id
terminal_id
command_id
operation
state
attempt_count
duration
error_class
server_request_id
```

Do not include sensitive payment data or secrets.

---

# 68. Metrics

Recommended:

```text
offline_queue_depth
offline_queue_age
sync_success_rate
sync_retry_rate
sync_conflict_rate
payment_unknown_count
reconciliation_age
offline_transaction_count
offline_transaction_amount
clock_drift_count
device_revocation_count
master_sync_age
```

---

# 69. Alerts

Potential alerts:

```text
queue age > threshold
payment unknown increasing
conflict rate increasing
terminal offline too long
master data stale
device repeatedly failing authentication
reconciliation backlog increasing
```

---

# 70. Testing

Mandatory tests:

### Crash

```text
crash after command persistence
crash before sync
crash during sync
crash after server success but before client response persistence
```

### Network

```text
offline
slow network
timeout
duplicate response
connection reset
```

### Idempotency

```text
same key/same payload
same key/different payload
```

### Conflict

```text
stock conflict
price conflict
promotion conflict
session conflict
device revoked
permission revoked
```

### Payment

```text
provider success + client timeout
provider failure + client timeout
duplicate retry
reconciliation
```

### Security

```text
tenant mismatch
store mismatch
terminal mismatch
user mismatch
revoked device
expired policy
clock drift
```

---

# 71. Acceptance Criteria

Offline DB v1 is accepted only if:

1. queued commands survive browser restart;
2. commands survive application crash;
3. idempotency keys never change during retry;
4. duplicate sync cannot create duplicate business effect;
5. payment ambiguity enters reconciliation;
6. conflicts are explicit;
7. server remains authoritative;
8. offline quota is enforced;
9. revoked devices cannot create new financial commands;
10. queued commands are not silently deleted;
11. master-data cache supports versioning;
12. promotions have immutable versions;
13. local timestamps are never treated as authoritative;
14. document numbers are server-generated;
15. sensitive payment credentials are not stored;
16. migrations preserve queue data;
17. two sync workers cannot process the same command concurrently;
18. permanently failing commands cannot poison the entire queue;
19. audit context is preserved;
20. telemetry supports operational diagnosis.

---

# 72. Architecture Locks

The following are locked for Offline Database v1:

### LOCK-OD-001

IndexedDB is the default browser persistence technology.

### LOCK-OD-002

Local database is not the system of record.

### LOCK-OD-003

Offline delivery is at-least-once.

### LOCK-OD-004

Business effect is exactly-once through server idempotency.

### LOCK-OD-005

Idempotency keys remain stable across retries.

### LOCK-OD-006

Payment ambiguity requires reconciliation.

### LOCK-OD-007

Conflicts are explicit and never silently overwritten.

### LOCK-OD-008

Device identity is separate from user identity.

### LOCK-OD-009

Server time is authoritative.

### LOCK-OD-010

Offline policy and quota are server-controlled.

### LOCK-OD-011

Offline promotion is limited to explicitly offline-safe versions.

### LOCK-OD-012

Loyalty/voucher redemption is disabled offline by default.

### LOCK-OD-013

Refund is disabled offline by default.

### LOCK-OD-014

ERP is never directly written by the local POS database.

### LOCK-OD-015

Authoritative business document numbers are generated by server.

### LOCK-OD-016

Local storage cannot override server authorization.

### LOCK-OD-017

Queued commands must survive browser restart and normal application migration.

### LOCK-OD-018

Audit metadata must remain associated with offline commands until server processing completes.

### LOCK-OD-019

No network request may remain inside an IndexedDB transaction.

### LOCK-OD-020

Local operational data must have explicit retention/cleanup policies.

---

# 73. Relationship to Other Architecture Artifacts

```text
Offline Sync Contract
        |
        v
Offline Database Design
        |
        +--> State Machine
        |
        +--> Permission Matrix
        |
        +--> Event Catalog
        |
        +--> Audit Action Catalog
        |
        +--> OpenAPI v1.1
```

All five must remain consistent.

If the offline command lifecycle changes, the following may require revision:

- State Machine Catalog;
- Permission Matrix;
- Event Catalog;
- Audit Action Catalog;
- OpenAPI;
- Go backend application layer.

---

# 74. Final Design Principle

The local POS database is intentionally designed as:

```text
DURABLE
      +
RECOVERABLE
      +
IDEMPOTENT
      +
CONFLICT-AWARE
      +
SECURE
      +
SERVER-AUTHORITATIVE
```

It is **not**:

```text
SECOND ERP
SECOND INVENTORY SYSTEM
SECOND PAYMENT SYSTEM
SECOND AUTHORIZATION SYSTEM
```

That distinction is fundamental to the architecture.
