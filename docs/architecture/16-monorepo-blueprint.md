# POS Monorepo / Repository Blueprint v1

## 1. Purpose

This document defines the production repository structure for the POS platform.

The repository must support:

- Go modular-monolith backend;
- Next.js/React POS frontend;
- PostgreSQL migrations;
- OpenAPI contract;
- offline sync contract;
- background workers;
- ERP/SAP adapters;
- payment provider adapters;
- automated tests;
- local development;
- CI/CD;
- future ecommerce/mobile clients;
- future extraction of selected modules without premature microservices.

The repository is organized around **domain ownership and deployable boundaries**, not database tables.

---

# 2. Architecture Principle

The repository follows:

```text
API
 ↓
Application
 ↓
Domain
 ↓
Ports
 ↓
Infrastructure
```

Frontend:

```text
UI
 ↓
Feature
 ↓
Application State
 ↓
API Client
 ↓
Offline Repository
```

Database:

```text
Migrations
 ↓
PostgreSQL
```

Integrations:

```text
Domain/Application
 ↓
Port
 ↓
Adapter
 ↓
External System
```

---

# 3. Repository Topology

Recommended monorepo:

```text
nexora/
│
├── apps/
│   ├── api/
│   ├── worker/
│   └── pos-web/
│
├── packages/
│   ├── api-contract/
│   ├── event-contract/
│   ├── typescript-client/
│   ├── ui/
│   ├── eslint-config/
│   └── tsconfig/
│
├── backend/
│   └── internal/
│
├── db/
│   ├── migrations/
│   ├── seeds/
│   └── fixtures/
│
├── contracts/
│   ├── openapi/
│   ├── events/
│   └── json-schema/
│
├── deploy/
│   ├── docker/
│   ├── compose/
│   ├── nginx/
│   └── environments/
│
├── scripts/
│
├── docs/
│   ├── architecture/
│   ├── adr/
│   ├── api/
│   ├── database/
│   ├── security/
│   └── runbooks/
│
├── tests/
│   ├── contract/
│   ├── e2e/
│   ├── performance/
│   └── security/
│
├── .github/
│   └── workflows/
│
├── Makefile
├── Taskfile.yml
├── docker-compose.yml
├── go.work
├── package.json
├── pnpm-workspace.yaml
├── turbo.json
├── README.md
└── .env.example
```

---

# 4. Why Monorepo

Monorepo is selected because:

- frontend and backend contracts evolve together;
- OpenAPI changes can be validated against frontend;
- event contracts are versioned centrally;
- database migrations live with application code;
- shared UI and TypeScript utilities are easier to manage;
- CI can detect cross-domain contract breakage;
- architecture documentation stays close to implementation.

Monorepo does not mean:

```text
everything shares everything
```

It means:

```text
one repository
+
explicit ownership boundaries
```

---

# 5. Deployable Applications

Initial deployables:

```text
api
worker
pos-web
```

Optional future:

```text
scheduler
reporting-worker
integration-worker
admin-web
ecommerce-web
mobile-api
```

Do not create separate deployables until operationally justified.

---

# 6. apps/api

Purpose:

Main Go HTTP API.

Structure:

```text
apps/api/
├── cmd/
│   └── api/
│       └── main.go
├── internal/
├── config/
├── Dockerfile
└── README.md
```

`main.go` must only compose the application.

It must not contain business logic.

---

# 7. apps/worker

Purpose:

Background processing.

Examples:

- transactional outbox delivery;
- payment reconciliation;
- ERP synchronization;
- webhook processing;
- notification;
- cleanup;
- reporting projection;
- retry processing.

Structure:

```text
apps/worker/
├── cmd/
│   └── worker/
│       └── main.go
├── internal/
└── Dockerfile
```

Worker shares domain/application packages with API.

---

# 8. apps/pos-web

Next.js application.

Structure:

```text
apps/pos-web/
├── app/
├── features/
├── components/
├── lib/
├── hooks/
├── stores/
├── offline/
├── api/
├── styles/
├── public/
├── tests/
├── next.config.ts
├── package.json
└── tsconfig.json
```

---

# 9. POS Frontend Feature Structure

Recommended:

```text
features/
├── auth/
├── terminal/
├── session/
├── cart/
├── checkout/
├── sales/
├── payment/
├── inventory/
├── customer/
├── promotion/
├── receipt/
├── offline-sync/
└── settings/
```

Each feature should own its UI/application behavior.

Avoid a global `utils` folder becoming a dumping ground.

---

# 10. Frontend Offline Boundary

```text
features/offline-sync/
├── application/
├── domain/
├── infrastructure/
└── ui/
```

Responsibilities:

```text
application
  sync orchestration

domain
  queue states
  conflict types

infrastructure
  IndexedDB
  network
  persistence

ui
  sync status
  conflict UX
```

---

# 11. Backend Root

Recommended:

```text
backend/
└── internal/
    ├── platform/
    ├── identity/
    ├── organization/
    ├── master/
    ├── crm/
    ├── pricing/
    ├── promotion/
    ├── sales/
    ├── pos/
    ├── payment/
    ├── inventory/
    ├── warehouse/
    ├── purchasing/
    ├── finance/
    ├── loyalty/
    ├── workflow/
    ├── integration/
    ├── audit/
    └── reporting/
```

Go package import paths should be stable.

---

# 12. Domain Module Template

Each domain follows:

```text
domain/
├── domain/
├── application/
├── ports/
├── infrastructure/
└── transport/
```

Example:

```text
sales/
├── domain/
│   ├── order.go
│   ├── item.go
│   ├── state.go
│   ├── command.go
│   └── errors.go
│
├── application/
│   ├── create_order.go
│   ├── confirm_order.go
│   ├── cancel_order.go
│   └── void_order.go
│
├── ports/
│   ├── repository.go
│   ├── pricing.go
│   ├── inventory.go
│   └── promotion.go
│
├── infrastructure/
│   └── postgres/
│
└── transport/
    └── http/
```

---

# 13. Domain Layer Rules

Domain layer may contain:

- entities;
- value objects;
- state machines;
- domain rules;
- domain errors;
- domain services where genuinely necessary.

Domain layer must not import:

```text
net/http
database/sql
pgx
Redis
Next.js
SAP SDK
payment provider SDK
```

---

# 14. Application Layer

Application layer coordinates use cases.

Example:

```text
ConfirmOrder
```

may:

1. authorize;
2. load aggregate;
3. validate state;
4. call domain command;
5. persist state;
6. create audit;
7. create outbox;
8. commit.

Application layer owns orchestration.

---

# 15. Ports

Ports define interfaces required by domain/application.

Examples:

```text
OrderRepository
InventoryPort
PricingPort
PromotionPort
PaymentPort
AuditWriter
EventPublisher
Clock
IdempotencyStore
```

Ports must describe business capability, not infrastructure.

Bad:

```text
PostgresRepository
```

Good:

```text
OrderRepository
```

---

# 16. Infrastructure

Infrastructure implements ports.

Example:

```text
sales/infrastructure/postgres/order_repository.go
```

Other examples:

```text
payment/infrastructure/xendit/
payment/infrastructure/nicepay/
payment/infrastructure/midtrans/

integration/infrastructure/sap/
integration/infrastructure/odoo/

platform/infrastructure/postgres/
platform/infrastructure/redis/
```

---

# 17. Cross-Domain Dependency Rules

Allowed:

```text
Sales
 ↓
Pricing Port
Inventory Port
Promotion Port
Payment Port
```

Not allowed:

```text
Sales
 ↓
promotion/infrastructure/postgres
```

Cross-domain access must use application/domain ports or published contracts.

---

# 18. Dependency Direction

Preferred:

```text
transport
   ↓
application
   ↓
domain
   ↓
ports

infrastructure
   └── implements ports
```

Never:

```text
domain → infrastructure
```

---

# 19. API Transport

Recommended:

```text
backend/internal/platform/transport/http/
```

Contains:

```text
router
middleware
request decoding
response encoding
error mapping
authentication context
request ID
```

Handlers should be thin.

---

# 20. Handler Rule

Handler may:

- parse request;
- validate basic schema;
- extract context;
- invoke use case;
- map result to HTTP.

Handler must not:

- execute business SQL;
- change state directly;
- call payment provider directly;
- calculate inventory;
- calculate promotion;
- write audit manually outside application transaction.

---

# 21. Middleware

Initial middleware:

```text
request_id
recovery
logging
authentication
authorization_context
tenant_context
rate_limit
body_limit
security_headers
```

RLS transaction context is applied at the database transaction boundary.

---

# 22. Authentication Context

Application context may contain:

```text
user_id
tenant_id
company_id
region_id
store_id
terminal_id
device_id
session_id
roles
permissions
request_id
trace_id
```

The client cannot arbitrarily override these values.

---

# 23. PostgreSQL Package

Recommended:

```text
backend/internal/platform/postgres/
```

Responsibilities:

- pgx pool;
- transactions;
- transaction context;
- RLS context;
- connection health;
- query tracing;
- retry only where safe.

---

# 24. Transaction Manager

Provide a small abstraction:

```text
WithinTransaction(ctx, func(txCtx context.Context) error)
```

Transaction setup:

```text
SET LOCAL app.tenant_id
SET LOCAL app.user_id
SET LOCAL app.store_id
SET LOCAL app.terminal_id
```

Exact settings depend on transaction context.

---

# 25. Repository Rule

Repositories must not become generic CRUD abstractions.

Prefer:

```text
FindOrderForConfirmation
SaveConfirmedOrder
GetOrderForVoid
```

where business semantics justify it.

Avoid:

```text
Update(table, map[string]any)
```

for critical domains.

---

# 26. Database Migrations

Structure:

```text
db/
├── migrations/
├── seeds/
└── fixtures/
```

Migration names:

```text
000001_init.sql
000002_identity.sql
000003_organization.sql
...
```

Migrations are immutable after merge.

Never edit a migration already applied to shared environments.

---

# 27. Migration Ownership

Database migration ownership belongs to the platform/database architecture layer.

Domain teams may contribute migrations for their domain.

Example:

```text
000101_sales_order.sql
000102_sales_payment.sql
000103_inventory_reservation.sql
```

---

# 28. Migration Rules

Every migration must be:

- deterministic;
- reversible where practical;
- tested;
- safe for deployment;
- compatible with RLS;
- compatible with indexes;
- documented for large-table operations.

---

# 29. Seed Data

Seeds must distinguish:

```text
development
test
demo
reference/master
```

Production seed must never contain fake business transactions.

---

# 30. OpenAPI Contract Location

Recommended:

```text
contracts/openapi/
├── openapi.yaml
├── components/
└── examples/
```

Alternative implementation may keep one canonical generated artifact.

Important:

There must be exactly one authoritative OpenAPI source.

---

# 31. Event Contracts

```text
contracts/events/
├── sales/
├── payment/
├── inventory/
├── promotion/
├── loyalty/
├── workflow/
├── integration/
└── platform/
```

Example:

```text
contracts/events/sales/sales-order-completed.v1.json
```

---

# 32. JSON Schema

Where useful:

```text
contracts/json-schema/
```

Use for:

- event validation;
- offline payload validation;
- integration payload validation.

OpenAPI schemas remain authoritative for HTTP API.

---

# 33. TypeScript Client

Recommended:

```text
packages/typescript-client/
```

Generated from OpenAPI.

Never manually edit generated files.

Pipeline:

```text
openapi.yaml
   ↓
generator
   ↓
TypeScript client
```

---

# 34. Shared TypeScript Types

Do not share backend Go domain types directly with frontend.

Frontend should consume:

```text
API contract
```

rather than an internal Go struct.

This preserves backend implementation freedom.

---

# 35. UI Package

```text
packages/ui/
```

Contains reusable:

- Button;
- Dialog;
- Table;
- Input;
- Select;
- Toast;
- Modal;
- Loading state;
- Error state.

POS-specific components stay inside `apps/pos-web`.

---

# 36. Configuration Packages

```text
packages/eslint-config/
packages/tsconfig/
```

Centralize frontend tooling without coupling business code.

---

# 37. API Contract Generation

Recommended workflow:

```text
contracts/openapi/openapi.yaml
          ↓
       validate
          ↓
       generate
          ↓
packages/typescript-client
          ↓
frontend compile
```

CI fails if generated artifacts are stale.

---

# 38. Event Contract Generation

Where practical:

```text
event schema
   ↓
validation
   ↓
generated consumer types
```

But event consumers must still validate runtime input.

---

# 39. Backend Tests

Each domain:

```text
domain/
  *_test.go

application/
  *_test.go

infrastructure/
  *_integration_test.go
```

Test layers:

```text
unit
integration
contract
concurrency
security
e2e
```

---

# 40. Frontend Tests

Recommended:

```text
unit
component
integration
offline
e2e
```

Critical offline tests:

```text
queue persistence
retry
duplicate sync
conflict
payment unknown
browser restart
migration
```

---

# 41. E2E Tests

```text
tests/e2e/
├── auth/
├── sales/
├── payment/
├── inventory/
├── promotion/
├── offline/
└── reconciliation/
```

---

# 42. Contract Tests

```text
tests/contract/
├── openapi/
├── events/
└── integration/
```

Verify:

```text
API implementation conforms to OpenAPI
event producer conforms to schema
consumer accepts supported event versions
```

---

# 43. Performance Tests

```text
tests/performance/
├── k6/
├── scenarios/
└── data/
```

Initial scenarios:

```text
login
product lookup
barcode lookup
cart
checkout
cash sale
payment
inventory
offline sync
master sync
```

---

# 44. Security Tests

```text
tests/security/
├── tenant-isolation/
├── rls/
├── authorization/
├── idempotency/
├── audit/
└── webhook/
```

Critical:

```text
cross-tenant access
cross-store access
cross-terminal access
role escalation
permission revocation
RLS connection pool leakage
```

---

# 45. Documentation Structure

```text
docs/
├── architecture/
│   ├── blueprint.md
│   ├── state-machines.md
│   ├── permission-matrix.md
│   ├── event-catalog.md
│   ├── audit-catalog.md
│   ├── offline-database.md
│   └── repository-blueprint.md
│
├── adr/
├── api/
├── database/
├── security/
└── runbooks/
```

The architecture artifacts created during design should be copied into repository documentation.

---

# 46. ADR Directory

```text
docs/adr/
├── ADR-0001-modular-monolith.md
├── ADR-0002-postgresql-system-of-record.md
├── ADR-0003-rls-authorization.md
├── ADR-0004-transactional-outbox.md
├── ADR-0005-payment-provider-abstraction.md
├── ADR-0006-erp-adapter.md
├── ADR-0007-inventory-concurrency.md
├── ADR-0008-idempotency.md
├── ADR-0009-audit.md
├── ADR-0010-offline-pos.md
└── ...
```

---

# 47. Configuration

Backend:

```text
.env
.env.example
```

Never commit secrets.

Configuration categories:

```text
HTTP
DATABASE
REDIS
AUTH
AUDIT
OUTBOX
PAYMENT
ERP
OBSERVABILITY
OFFLINE
```

---

# 48. Secret Management

Local development:

```text
.env.local
```

Production:

Use deployment secret manager.

Do not store:

```text
API keys
database passwords
JWT signing keys
payment secrets
SAP credentials
encryption keys
```

in Git.

---

# 49. Docker

Recommended:

```text
deploy/docker/
├── api.Dockerfile
├── worker.Dockerfile
└── pos-web.Dockerfile
```

Multi-stage builds.

Runtime containers should contain only required artifacts.

---

# 50. Local Development Compose

```text
docker-compose.yml
```

Initial services:

```text
postgres
redis
api
worker
pos-web
```

Optional:

```text
mailpit
otel-collector
```

---

# 51. Production Topology

Initial:

```text
Internet
   |
Cloudflare / Reverse Proxy
   |
   +--------------------+
   |                    |
POS Web               API
                         |
                +--------+--------+
                |                 |
             PostgreSQL         Redis
                |
              Worker
                |
        +-------+-------+
        |               |
       ERP           Payment
```

The exact cloud topology remains deployment-specific.

---

# 52. Redis Boundary

Redis may be used for:

- cache;
- rate limiting;
- ephemeral coordination;
- distributed locks only where justified.

Redis must not become:

- inventory source of truth;
- payment source of truth;
- authoritative session store without explicit design;
- financial ledger.

---

# 53. Background Worker Modules

Worker jobs:

```text
outbox_delivery
payment_reconciliation
erp_delivery
webhook_processing
notification
reporting_projection
offline_reconciliation
cleanup
```

Each job must be idempotent.

---

# 54. Outbox Worker

Flow:

```text
outbox
 ↓
claim
 ↓
publish/deliver
 ↓
success
 OR
retry
 OR
dead-letter
```

Use leases to prevent duplicate processing.

---

# 55. Payment Worker

Must support:

```text
provider timeout
unknown result
webhook
reconciliation
retry
```

Provider-specific logic remains inside adapters.

---

# 56. ERP Worker

Example:

```text
SalesCompleted
   ↓
ERP adapter
   ↓
SAP B1
```

ERP failure must not roll back an already committed POS transaction.

Use:

```text
retry
reconciliation
dead-letter
```

---

# 57. Domain Module Ownership

Suggested ownership:

```text
identity       Identity team/platform
organization   Platform
master         Master Data
crm            Customer
pricing        Commercial
promotion      Commercial
sales          Sales
pos            Retail POS
payment        Payments
inventory      Inventory
warehouse      Warehouse
purchasing     Procurement
finance        Finance
loyalty        CRM/Loyalty
workflow       Platform
integration    Integration
audit          Security/Platform
reporting      Data/Reporting
```

Actual team ownership can differ.

---

# 58. Module Boundary Rule

A domain owns:

```text
business rules
aggregate lifecycle
commands
repositories
domain events
```

Other domains consume capabilities through:

```text
ports
application contracts
events
```

---

# 59. Database Ownership Rule

A domain owns its tables.

Example:

```text
sales owns sales.sales_order
inventory owns inventory.stock
payment owns payment.payment
promotion owns promotion.promotion
```

Another domain must not directly mutate those tables.

---

# 60. Read Model Exception

Cross-domain reporting may use:

```text
reporting
```

read models.

Reporting queries must not become an excuse to bypass domain ownership for transactional writes.

---

# 61. Shared Platform Module

`platform` contains cross-cutting capabilities:

```text
config
clock
id
request context
transaction
postgres
redis
logging
tracing
metrics
idempotency
outbox
security
pagination
errors
```

Do not place business rules here.

---

# 62. Error Package

Central error taxonomy may contain:

```text
DomainError
ValidationError
AuthorizationError
ConflictError
NotFoundError
RetryableError
ExternalError
```

Domain-specific codes remain owned by domains.

---

# 63. Logging

Structured JSON logs.

Required fields where available:

```text
timestamp
level
service
environment
request_id
trace_id
tenant_id
user_id
store_id
terminal_id
operation
error_code
duration_ms
```

Never log:

```text
password
CVV
full PAN
access token
refresh token
API secret
encryption key
MFA secret
```

---

# 64. Observability

Use:

```text
OpenTelemetry
```

for:

- traces;
- metrics;
- logs correlation.

Initial trace:

```text
HTTP request
 ↓
application command
 ↓
DB transaction
 ↓
outbox
 ↓
worker
 ↓
external provider
```

---

# 65. Health Endpoints

API:

```http
GET /health/live
GET /health/ready
```

Live:

```text
process is alive
```

Ready:

```text
required dependencies are available
```

Do not make liveness depend on PostgreSQL.

---

# 66. Graceful Shutdown

API:

```text
stop accepting requests
↓
finish in-flight requests
↓
close workers
↓
close DB
```

Worker:

```text
stop claiming jobs
↓
finish current safe work
↓
release leases
↓
shutdown
```

---

# 67. Local Developer Commands

Recommended:

```text
make dev
make test
make test-unit
make test-integration
make test-e2e
make lint
make fmt
make generate
make migrate
make seed
make openapi-validate
make openapi-generate
make docker-build
```

Equivalent Taskfile commands may be provided.

---

# 68. Go Tooling

Recommended:

```text
go test
go vet
gofmt
staticcheck
golangci-lint
```

Exact versions pinned in CI/tooling configuration.

---

# 69. Frontend Tooling

Recommended:

```text
pnpm
TypeScript
ESLint
Prettier
Next.js
React
Playwright
Vitest/Jest
```

Exact versions should be pinned.

---

# 70. Package Manager

Recommended frontend package manager:

```text
pnpm
```

Workspace:

```text
pnpm-workspace.yaml
```

Optional build orchestration:

```text
Turborepo
```

Use it only if build complexity justifies it.

---

# 71. Go Workspace

`go.work` may be used if the repository contains multiple Go modules.

Initial preference:

```text
one Go module
```

rather than many micro-modules.

---

# 72. Go Module Strategy

Prefer:

```text
go.mod
```

at repository/backend root as appropriate.

Avoid creating one `go.mod` per domain.

This keeps the modular monolith simpler.

---

# 73. Frontend Environment

Example:

```text
NEXT_PUBLIC_API_BASE_URL
NEXT_PUBLIC_APP_VERSION
NEXT_PUBLIC_ENVIRONMENT
```

Never expose secrets through `NEXT_PUBLIC_*`.

---

# 74. API URL Strategy

Frontend should use:

```text
NEXT_PUBLIC_API_BASE_URL
```

or same-origin proxying depending on deployment.

Do not hardcode production API URL in source.

---

# 75. Feature Flags

Feature flags may control:

```text
offline
new checkout
new promotion engine
new payment provider
```

But security permissions must not be implemented as frontend feature flags.

---

# 76. Database Compatibility

Application code must support:

```text
current migration version
```

during rolling deployment where necessary.

Prefer backward-compatible migrations:

```text
expand
→ migrate
→ contract
```

for high-availability deployments.

---

# 77. Deployment Migration Pattern

For breaking schema changes:

```text
1. Add new column/table
2. Deploy compatible code
3. Backfill
4. Switch reads/writes
5. Remove old structure later
```

Avoid:

```text
drop column
+
deploy code
```

in a single risky operation.

---

# 78. Repository Security

Protect:

```text
main
release/*
```

Require:

- pull request;
- review;
- CI;
- security checks;
- migration checks;
- OpenAPI checks.

---

# 79. CODEOWNERS

Suggested ownership:

```text
/backend/internal/payment/       @payments
/backend/internal/inventory/     @inventory
/backend/internal/sales/         @sales
/contracts/openapi/               @platform
/db/migrations/                   @platform
/docs/security/                   @security
```

Actual teams may differ.

---

# 80. Pull Request Rules

PR should include:

```text
business impact
architecture impact
database migration
API changes
event changes
permission changes
audit changes
offline impact
test evidence
```

---

# 81. Architecture Change Checklist

Any change to:

```text
state machine
permission
event contract
audit action
offline operation
API contract
database ownership
payment state
inventory concurrency
```

must trigger architecture review.

---

# 82. Generated Files

Generated files:

```text
packages/typescript-client/generated/
```

must contain a header:

```text
DO NOT EDIT — GENERATED
```

Source remains:

```text
contracts/openapi/
```

---

# 83. Repository Naming

Use:

```text
snake_case
```

for PostgreSQL.

Use:

```text
PascalCase
```

for Go types.

Use:

```text
camelCase
```

for TypeScript/JSON fields unless API contract specifies otherwise.

API JSON remains consistent with OpenAPI.

---

# 84. Commit Convention

Recommended:

```text
feat:
fix:
refactor:
docs:
test:
perf:
build:
ci:
chore:
security:
```

Example:

```text
feat(sales): add offline cash sale command
```

---

# 85. Branch Strategy

Recommended simple strategy:

```text
main
feature/*
bugfix/*
release/*
```

Avoid complex GitFlow unless organization requires it.

---

# 86. Release Versioning

Application:

```text
MAJOR.MINOR.PATCH
```

API contract:

```text
/api/v1
```

Event:

```text
event_type + event_version
```

Database:

```text
migration version
```

These version systems remain independent.

---

# 87. Monorepo Dependency Rules

Allowed:

```text
apps/pos-web
    ↓
packages/typescript-client
packages/ui
```

```text
apps/api
    ↓
backend/internal/*
```

Forbidden:

```text
frontend → backend internal Go package
```

and:

```text
domain → frontend/shared UI
```

---

# 88. Circular Dependency Prevention

CI should detect:

```text
domain A → domain B → domain A
```

If cross-domain dependency becomes circular, introduce:

```text
port
application contract
event
```

rather than direct import.

---

# 89. Future Ecommerce

Future:

```text
apps/ecommerce-web
```

can consume:

```text
packages/typescript-client
packages/ui
```

and use the same domain API.

POS-specific behavior stays inside POS feature modules.

---

# 90. Future Mobile

Future:

```text
apps/mobile
```

uses:

```text
OpenAPI contract
```

without importing backend implementation.

---

# 91. Future ERP Expansion

ERP adapters:

```text
backend/internal/integration/
├── sap_b1/
├── sap_s4/
├── odoo/
└── ...
```

Core domains remain ERP-neutral.

---

# 92. Future Payment Expansion

Payment adapters:

```text
backend/internal/payment/infrastructure/
├── xendit/
├── midtrans/
├── nicepay/
└── ...
```

Payment domain depends on:

```text
PaymentProvider interface
```

not provider SDKs.

---

# 93. Future Service Extraction

If one module eventually needs independent scaling:

```text
inventory
payment
integration
```

may be extracted.

The current repository boundaries make extraction possible without requiring microservices today.

---

# 94. Anti-Patterns

Forbidden:

```text
god_service.go
god_repository.go
utils_everything.go
database.go with all queries
handler.go with business rules
```

Also forbidden:

```text
frontend directly reading PostgreSQL
frontend directly calling SAP
sales module directly calling Xendit
inventory module directly mutating payment tables
```

---

# 95. Recommended Initial Backend Tree

```text
backend/
└── internal/
    ├── platform/
    │   ├── config/
    │   ├── context/
    │   ├── errors/
    │   ├── http/
    │   ├── idempotency/
    │   ├── observability/
    │   ├── postgres/
    │   ├── redis/
    │   ├── transaction/
    │   └── outbox/
    │
    ├── identity/
    ├── organization/
    ├── master/
    ├── crm/
    ├── pricing/
    ├── promotion/
    ├── sales/
    ├── pos/
    ├── payment/
    ├── inventory/
    ├── warehouse/
    ├── purchasing/
    ├── finance/
    ├── loyalty/
    ├── workflow/
    ├── integration/
    ├── audit/
    └── reporting/
```

---

# 96. Recommended Initial Frontend Tree

```text
apps/pos-web/
├── app/
│   ├── login/
│   ├── pos/
│   └── settings/
│
├── features/
│   ├── auth/
│   ├── terminal/
│   ├── session/
│   ├── cart/
│   ├── checkout/
│   ├── payment/
│   ├── customer/
│   ├── promotion/
│   ├── receipt/
│   └── offline-sync/
│
├── components/
├── hooks/
├── stores/
├── offline/
├── api/
├── lib/
├── tests/
└── public/
```

---

# 97. Recommended Contract Tree

```text
contracts/
├── openapi/
│   ├── openapi.yaml
│   └── examples/
│
├── events/
│   ├── sales/
│   ├── payment/
│   ├── inventory/
│   ├── promotion/
│   ├── loyalty/
│   ├── workflow/
│   ├── integration/
│   └── platform/
│
└── json-schema/
```

---

# 98. Recommended Test Tree

```text
tests/
├── contract/
├── e2e/
├── performance/
├── security/
└── fixtures/
```

Domain-specific tests remain close to implementation.

---

# 99. Definition of Done — Repository Foundation

Repository foundation is accepted when:

1. API builds;
2. worker builds;
3. POS web builds;
4. PostgreSQL migrations run;
5. Redis starts;
6. local compose works;
7. OpenAPI validates;
8. TypeScript client generates;
9. Go tests run;
10. frontend tests run;
11. lint passes;
12. formatting passes;
13. security checks run;
14. contract tests run;
15. Docker images build;
16. health endpoints work;
17. structured logging works;
18. tracing can be enabled;
19. environment configuration is documented;
20. architecture boundaries are enforced.

---

# 100. Architecture Locks

### LOCK-REPO-001

Monorepo is the source repository for the initial platform.

### LOCK-REPO-002

Backend remains a modular monolith.

### LOCK-REPO-003

API, worker, and POS web are separate deployables.

### LOCK-REPO-004

Domain ownership is explicit.

### LOCK-REPO-005

Cross-domain database writes are forbidden.

### LOCK-REPO-006

Frontend consumes contracts, not backend internals.

### LOCK-REPO-007

OpenAPI has one authoritative source.

### LOCK-REPO-008

Event contracts have one authoritative source.

### LOCK-REPO-009

Database migrations are immutable after shared deployment.

### LOCK-REPO-010

Generated clients are never manually edited.

### LOCK-REPO-011

Provider-specific implementations stay behind adapters.

### LOCK-REPO-012

ERP-specific concepts stay behind integration adapters unless they are genuine core business concepts.

### LOCK-REPO-013

Redis is not a source of truth.

### LOCK-REPO-014

Shared platform code cannot contain domain business rules.

### LOCK-REPO-015

Repository structure must preserve future service-extraction boundaries without prematurely creating microservices.

---

# 101. Final Architecture

```text
                         MONOREPO
                            |
          +-----------------+------------------+
          |                 |                  |
       pos-web             api              worker
          |                 |                  |
          |          application/domain        |
          |                 |                  |
          |                 +--------+---------+
          |                          |
          |                       ports
          |                          |
          |                     infrastructure
          |                          |
          |                     PostgreSQL
          |                          |
          |                       Outbox
          |                          |
          |                        Worker
          |                          |
          |                +---------+---------+
          |                |                   |
          |               ERP               Payment
          |
      IndexedDB
          |
     Offline Sync
          |
          +------------> API
```

The repository deliberately keeps:

```text
one codebase
+
strong domain boundaries
+
independent deployables
+
versioned contracts
+
future extraction capability
```

without introducing microservice operational complexity before the platform needs it.
