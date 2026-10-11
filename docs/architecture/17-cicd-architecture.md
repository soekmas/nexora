# POS CI/CD Architecture v1

## 1. Purpose

This document defines the CI/CD architecture for the POS platform.

The pipeline must protect the architecture already locked across:

- modular monolith;
- PostgreSQL system of record;
- RLS and application authorization;
- state machines;
- permission matrix;
- transactional outbox;
- audit;
- offline sync;
- OpenAPI;
- event contracts;
- domain boundaries;
- database migrations;
- payment/ERP adapters.

The objective is:

```text
fast feedback
+
repeatable builds
+
security
+
contract safety
+
migration safety
+
deployability
+
rollback capability
```

---

# 2. CI/CD Principles

1. Every merge must be reproducible.
2. Production artifacts must come from CI.
3. Production should never be built manually.
4. Tests must run before deployment.
5. Database migrations must be validated independently.
6. OpenAPI changes must be checked for breaking changes.
7. Event contracts must be version-checked.
8. Security scans are mandatory.
9. Critical architecture changes require review.
10. Deployment must be observable.
11. Rollback must be designed before release.
12. Secrets must never exist in Git.
13. Database rollback must not rely on blindly reversing migrations.
14. Production deployment must use immutable artifacts.
15. CI should fail fast on cheap checks and reserve expensive tests for later stages.

---

# 3. Pipeline Overview

```text
Developer
   |
   v
Pull Request
   |
   +--> Format / Lint
   +--> Unit Tests
   +--> Static Analysis
   +--> Security Scan
   +--> OpenAPI Validation
   +--> Event Contract Validation
   +--> Architecture Checks
   +--> DB Migration Validation
   +--> Build
   +--> Integration Tests
   +--> Contract Tests
   +--> E2E
   |
   v
Merge to Main
   |
   v
Build Immutable Artifacts
   |
   +--> API Image
   +--> Worker Image
   +--> POS Web Image
   |
   v
Artifact Registry
   |
   v
Deploy Staging
   |
   +--> Smoke
   +--> Integration
   +--> E2E
   +--> Security
   |
   v
Approval
   |
   v
Production
   |
   +--> Migration
   +--> API
   +--> Worker
   +--> POS Web
   |
   v
Post-Deploy Verification
   |
   +--> Metrics
   +--> Health
   +--> Smoke
   +--> Rollback if necessary
```

---

# 4. CI vs CD

## CI

Runs on:

```text
pull_request
push to main
```

CI validates source code and creates artifacts.

## CD

Runs after successful artifact creation.

Typical environments:

```text
development
staging
production
```

---

# 5. Pull Request Pipeline

Every PR should execute:

```text
1. Detect changes
2. Format
3. Lint
4. Unit tests
5. Static analysis
6. Dependency scan
7. Secret scan
8. OpenAPI validation
9. Event contract validation
10. Architecture boundary checks
11. DB migration validation
12. Build
13. Integration tests
14. Contract tests
15. Security tests
```

E2E may run in parallel or after integration tests depending on CI cost.

---

# 6. Change Detection

CI should determine whether changes affect:

```text
backend
frontend
database
contracts
events
docs
deployment
security
```

Examples:

Only documentation changes:

```text
docs/**
```

should not trigger the full production build.

Database change:

```text
db/migrations/**
```

must trigger migration tests.

OpenAPI change:

```text
contracts/openapi/**
```

must trigger contract validation and breaking-change detection.

---

# 7. Formatting

Go:

```text
gofmt
goimports
```

Frontend:

```text
prettier
eslint
```

CI must fail if formatting differs from repository standard.

---

# 8. Go Static Analysis

Minimum:

```text
go vet
staticcheck
golangci-lint
```

Check:

- nil handling;
- concurrency;
- error handling;
- unreachable code;
- suspicious constructs;
- complexity;
- security-sensitive patterns.

---

# 9. TypeScript Checks

Minimum:

```text
tsc --noEmit
eslint
prettier --check
```

---

# 10. Unit Tests

Go:

```text
go test ./...
```

Frontend:

```text
pnpm test
```

Coverage is a quality signal, not the only quality gate.

Critical domains require meaningful tests:

```text
sales
payment
inventory
promotion
identity
offline-sync
```

---

# 11. Critical Business Test Requirement

Changes to:

```text
payment
inventory
promotion
sales state machine
permission
authorization
offline sync
```

must include focused regression tests.

A PR changing a critical rule without tests should fail review.

---

# 12. Architecture Boundary Checks

CI should detect forbidden imports.

Examples:

```text
domain → infrastructure
domain → HTTP
sales → payment provider SDK
sales → SAP SDK
frontend → Go internal
```

Allowed:

```text
application → domain
infrastructure → ports
transport → application
```

---

# 13. Go Package Boundary Validation

Use static analysis or custom scripts to enforce:

```text
internal/<domain>/domain
```

cannot import:

```text
database
HTTP
provider SDK
```

Custom architecture tests may be added under:

```text
tests/architecture/
```

---

# 14. OpenAPI Validation

CI must validate:

```text
contracts/openapi/openapi.yaml
```

Checks:

- syntax;
- references;
- schemas;
- operation IDs;
- examples;
- security schemes;
- duplicate definitions.

---

# 15. OpenAPI Breaking Change Detection

Compare:

```text
base branch OpenAPI
vs
PR OpenAPI
```

Fail on breaking changes unless explicitly approved.

Examples of breaking changes:

- removing endpoint;
- removing required response field;
- changing field type;
- changing required request field;
- incompatible enum removal;
- changing authentication semantics.

---

# 16. Event Contract Validation

Validate:

```text
contracts/events/**
```

against JSON Schema.

Check:

- required fields;
- version;
- payload structure;
- event naming;
- examples.

---

# 17. Event Compatibility

Existing event version:

```text
SalesOrderCompleted.v1
```

must remain compatible for existing consumers.

Breaking changes should create:

```text
SalesOrderCompleted.v2
```

rather than silently modifying v1 semantics.

---

# 18. Database Migration CI

Every migration PR must run against a clean PostgreSQL instance.

Flow:

```text
empty database
      ↓
all migrations
      ↓
schema verification
      ↓
seed
      ↓
tests
```

---

# 19. Migration Upgrade Test

Test:

```text
previous production-like schema
        ↓
new migrations
        ↓
application tests
```

Use a production-like snapshot where permitted.

---

# 20. Migration Safety

CI should detect:

- destructive DROP;
- incompatible type changes;
- large table rewrite;
- blocking index creation;
- unsafe NOT NULL;
- missing indexes for new FKs;
- missing RLS policy;
- permission mistakes.

Destructive migrations require explicit approval.

---

# 21. Expand / Contract

Production schema changes follow:

```text
EXPAND
  ↓
COMPATIBLE APPLICATION
  ↓
BACKFILL
  ↓
SWITCH
  ↓
CONTRACT
```

Avoid:

```text
DROP COLUMN
+
new application
```

in one release.

---

# 22. Database Rollback Principle

Application rollback:

```text
previous container/image
```

Database rollback:

```text
forward-compatible migration
```

Do not depend on:

```text
rollback SQL
```

for every production incident.

If a migration is destructive, rollback strategy must be documented before approval.

---

# 23. RLS CI

Automated tests must verify:

```text
tenant A cannot read tenant B
tenant A cannot write tenant B
store A cannot access store B
terminal A cannot act as terminal B
```

Also test pooled connections:

```text
request A context
→ transaction
→ request B
```

to detect leaked session variables.

---

# 24. Security Scanning

CI should run:

### SAST

Static application security analysis.

### Dependency scan

Detect vulnerable packages.

### Secret scan

Detect:

```text
API keys
tokens
passwords
private keys
payment secrets
```

### Container scan

Scan Docker images.

---

# 25. Dependency Management

Dependencies must be pinned.

Go:

```text
go.mod
go.sum
```

Node:

```text
pnpm-lock.yaml
```

Unexpected dependency changes should be visible in PR review.

---

# 26. Container Build

Images:

```text
pos-api
pos-worker
pos-web
```

Use multi-stage builds.

Example:

```text
builder
   ↓
minimal runtime
```

---

# 27. Container Security

Runtime images should:

- use minimal base;
- run non-root where practical;
- contain no build tools;
- contain no source secrets;
- have predictable entrypoint;
- expose only required ports.

---

# 28. Immutable Artifacts

Each build receives immutable identifier:

```text
git SHA
```

Example:

```text
pos-api:8c1f4d2
pos-worker:8c1f4d2
pos-web:8c1f4d2
```

Do not deploy mutable:

```text
latest
```

to production.

---

# 29. Artifact Registry

Recommended:

```text
container registry
```

Stores:

```text
API image
Worker image
POS Web image
```

Artifacts must be retained according to release policy.

---

# 30. Image Signing

Production images should be signed.

Conceptual flow:

```text
build
 ↓
scan
 ↓
sign
 ↓
registry
 ↓
deployment verifies signature
```

Tools such as Cosign may be used.

---

# 31. SBOM

Generate Software Bill of Materials.

Recommended format:

```text
SPDX
or
CycloneDX
```

Attach SBOM to release artifact.

---

# 32. Provenance

Release should record:

```text
git SHA
build time
builder
dependencies
image digest
migration version
OpenAPI version
event contract version
```

This allows production traceability.

---

# 33. Staging Deployment

After merge:

```text
build
 ↓
push artifacts
 ↓
deploy staging
```

Staging should use:

- production-like configuration;
- production-like PostgreSQL version;
- realistic dataset volume;
- Redis;
- payment sandbox;
- ERP sandbox where available.

---

# 34. Staging Migration

Run:

```text
migration
 ↓
application deployment
```

only after migration compatibility validation.

For expand/contract:

```text
expand migration
 ↓
compatible app
```

---

# 35. Staging Smoke Test

Immediately after deployment:

```text
GET /health/live
GET /health/ready
```

Then:

```text
login
product lookup
create cart
cash sale
payment sandbox
inventory
offline sync
```

---

# 36. Staging E2E

Minimum:

```text
login
open POS session
scan product
add cart
promotion
checkout
cash payment
sale completion
inventory deduction
receipt
audit
outbox
```

Offline:

```text
disconnect network
create cash sale
reconnect
sync
verify server result
```

---

# 37. Production Approval

Production deployment should require approval for:

- database migration;
- payment changes;
- permission changes;
- state machine changes;
- security changes;
- infrastructure changes.

Low-risk frontend-only changes may follow automated deployment policy.

---

# 38. Production Deployment Strategy

Initial recommended strategy:

```text
migration
 ↓
deploy API
 ↓
verify
 ↓
deploy worker
 ↓
verify
 ↓
deploy POS Web
 ↓
verify
```

Exact order depends on migration compatibility.

---

# 39. API Deployment

Use rolling deployment where infrastructure supports it.

Requirements:

```text
graceful shutdown
readiness probe
health checks
connection draining
```

---

# 40. Worker Deployment

Workers should support:

```text
multiple replicas
job lease
idempotency
graceful shutdown
```

Never assume only one worker instance exists.

---

# 41. POS Web Deployment

Next.js artifact should be immutable.

Deployment should expose:

```text
app version
git SHA
build ID
```

for troubleshooting.

---

# 42. Zero-Downtime Rule

Deployments must preserve service availability unless an explicit maintenance window is approved.

Database changes must therefore follow expand/contract.

---

# 43. Rollback

Application rollback:

```text
deploy previous image digest
```

Frontend rollback:

```text
previous immutable web artifact
```

Database:

```text
forward fix
```

unless a tested rollback migration exists.

---

# 44. Automatic Rollback Conditions

Potential triggers:

```text
health check failure
5xx spike
latency spike
payment failure spike
DB error spike
outbox failure spike
worker crash loop
```

Automatic rollback should be used carefully for financial systems.

Prefer alert + controlled rollback for payment/database changes.

---

# 45. Post-Deployment Verification

After production deployment:

```text
health
latency
5xx
database
worker
outbox
payment
inventory
offline sync
```

Check:

```text
queue depth
error rate
payment unknown
reconciliation backlog
```

---

# 46. Production Smoke

Minimum:

```text
authentication
product lookup
sale creation
cash payment
audit verification
outbox verification
```

Payment gateway production smoke must use approved safe test strategy.

---

# 47. Deployment Observability

Every deployment should emit:

```text
deployment_started
deployment_succeeded
deployment_failed
deployment_rolled_back
```

Metadata:

```text
service
version
git_sha
environment
operator
timestamp
```

---

# 48. CI/CD Audit

Production deployment must be auditable.

Record:

```text
who
what
when
which version
which artifact
which environment
approval
result
```

This integrates with the Audit Action Catalog.

---

# 49. Release Metadata

Release artifact should include:

```text
version
git_sha
build_id
image_digest
database_migration_version
openapi_contract_version
event_contract_version
```

---

# 50. Release Notes

Every release should identify:

```text
features
bug fixes
database changes
API changes
event changes
permission changes
security changes
offline changes
migration notes
rollback notes
```

---

# 51. Semantic Versioning

Application:

```text
MAJOR.MINOR.PATCH
```

API path:

```text
/api/v1
```

Event:

```text
EventType.vN
```

Database:

```text
migration sequence
```

Do not confuse these versioning systems.

---

# 52. Feature Flags

Feature flags may support controlled rollout:

```text
offline_v1
new_promotion_engine
new_payment_provider
```

But:

```text
authorization
security controls
audit
```

must not depend solely on frontend feature flags.

---

# 53. Canary

For critical backend changes, future deployment may use:

```text
canary
```

Example:

```text
5%
 ↓
15%
 ↓
50%
 ↓
100%
```

Monitor:

```text
5xx
latency
payment failures
inventory conflicts
```

---

# 54. Blue/Green

Blue/green may be introduced later if infrastructure requires fast application rollback.

Database compatibility remains mandatory.

---

# 55. Environment Strategy

```text
development
    ↓
staging
    ↓
production
```

Environment differences must be configuration, not code forks.

---

# 56. Environment Isolation

Each environment should have separate:

```text
database
Redis
payment credentials
ERP credentials
API secrets
observability
```

Never use production credentials in staging.

---

# 57. Production Data

Production data must not be copied into developer environments without approved anonymization.

Sensitive fields must be masked.

---

# 58. Test Data

Use deterministic fixtures for:

```text
products
prices
promotions
customers
users
stores
inventory
```

Payment provider sandbox data should be used for payment tests.

---

# 59. CI Database

Use ephemeral PostgreSQL for:

```text
unit/integration tests
migration tests
RLS tests
repository tests
```

---

# 60. Redis CI

Use ephemeral Redis where tests depend on:

```text
cache
rate limit
lease
distributed coordination
```

Tests must not rely on a developer's external Redis.

---

# 61. Integration Test Architecture

```text
Test Runner
   |
   +--> PostgreSQL
   +--> Redis
   +--> API
   +--> Worker
   +--> Mock Payment
   +--> Mock ERP
```

External providers should be mocked/sandboxed.

---

# 62. Payment Integration Tests

Must cover:

```text
success
declined
timeout
unknown
duplicate webhook
invalid signature
retry
refund
```

---

# 63. ERP Integration Tests

Must cover:

```text
success
timeout
5xx
validation failure
duplicate submission
retry
dead-letter
reconciliation
```

---

# 64. Webhook Tests

Verify:

```text
signature
timestamp
replay
duplicate event
unknown event
out-of-order event
```

---

# 65. Offline CI

Frontend offline tests must simulate:

```text
network offline
network restored
server timeout
duplicate sync
conflict
payment unknown
browser restart
IndexedDB migration
```

---

# 66. Performance Gates

Initial thresholds should be defined empirically.

Monitor:

```text
API p95 latency
API p99 latency
DB query latency
checkout latency
sync throughput
outbox throughput
worker lag
```

Do not invent hard limits without baseline measurement.

---

# 67. Load Testing

Use:

```text
k6
```

Initial scenarios:

```text
100 concurrent
300 concurrent
500 concurrent
```

depending on the production target.

Focus on:

```text
product lookup
cart
checkout
sale
payment
inventory
offline sync
```

---

# 68. Concurrency Testing

Mandatory:

```text
two users buy last item
two payment requests same idempotency key
two refunds same payment
two stock adjustments
two promotion approvals
two terminal sessions
```

CI should run deterministic concurrency tests where practical.

---

# 69. Security Gate

Production release must not proceed if:

```text
critical vulnerability
secret detected
critical SAST finding
critical container vulnerability
failed RLS isolation test
failed authorization test
```

Exception requires explicit security approval.

---

# 70. Dependency Vulnerability Policy

Suggested:

```text
Critical = block
High = block or explicit exception
Medium = tracked
Low = tracked
```

Exact policy belongs to security governance.

---

# 71. Secret Scanning

Scan:

```text
commits
PR diff
repository
Docker build context
CI logs
```

If secret detected:

```text
block
rotate credential
remove secret
audit exposure
```

Deleting the Git line alone is not enough if the secret was exposed.

---

# 72. CI Secret Access

CI jobs should receive only required secrets.

Prefer:

```text
OIDC
short-lived credentials
```

over static long-lived credentials where supported.

---

# 73. Deployment Permissions

Separate:

```text
developer
release manager
production deployer
database administrator
security administrator
```

Production deployment should follow least privilege.

---

# 74. Database Migration Permission

Not every developer should have unrestricted production DB migration privileges.

Prefer:

```text
CI deployment identity
```

with controlled permissions.

---

# 75. Break-Glass Deployment

Emergency production access requires:

```text
MFA
reason
ticket/reference
time-limited access
audit
post-incident review
```

---

# 76. CI/CD Failure Handling

If CI fails:

```text
merge blocked
```

If staging fails:

```text
production blocked
```

If production deployment fails:

```text
stop rollout
diagnose
rollback application if safe
```

---

# 77. Deployment Lock

Prevent concurrent production deployments.

Use:

```text
environment lock
```

so:

```text
Release A
```

and:

```text
Release B
```

cannot modify production simultaneously.

---

# 78. Database Migration Lock

Use migration tool locking to prevent two migration processes from executing concurrently.

Never allow:

```text
migration process A
+
migration process B
```

against the same production DB simultaneously.

---

# 79. Worker Migration Compatibility

When deploying a new worker version:

```text
old worker
+
new worker
```

may temporarily coexist.

Therefore:

- job schema must be compatible;
- event schemas must be compatible;
- database schema must be backward compatible.

---

# 80. Event Consumer Compatibility

During rolling deployment:

```text
consumer v1
+
consumer v2
```

may coexist.

Events must therefore remain compatible across deployment window.

---

# 81. Outbox Deployment Safety

Outbox schema changes must follow:

```text
expand
 ↓
deploy compatible worker
 ↓
switch
 ↓
contract
```

Never remove columns still consumed by older workers.

---

# 82. Release Candidate

For major releases:

```text
main
 ↓
release candidate
 ↓
staging
 ↓
approval
 ↓
production
```

RC artifact should be identical to production artifact.

---

# 83. Artifact Promotion

Do not rebuild between environments.

Correct:

```text
build once
 ↓
artifact
 ↓
staging
 ↓
production
```

Incorrect:

```text
build staging
 ↓
rebuild production
```

---

# 84. Git SHA Traceability

Every runtime should expose:

```text
version
git_sha
build_id
```

Example:

```http
GET /health/version
```

Response:

```json
{
  "service": "pos-api",
  "version": "1.0.0",
  "git_sha": "8c1f4d2",
  "build_id": "build-123"
}
```

---

# 85. Incident Workflow

Production incident:

```text
detect
 ↓
triage
 ↓
contain
 ↓
rollback/fix
 ↓
verify
 ↓
communicate
 ↓
postmortem
```

---

# 86. Postmortem Requirements

For significant incidents:

```text
timeline
impact
root cause
detection
mitigation
resolution
architecture gap
action items
owner
deadline
```

---

# 87. CI/CD Metrics

Track:

```text
deployment frequency
lead time for changes
change failure rate
MTTR
pipeline duration
test failure rate
flaky test rate
rollback rate
security finding rate
migration failure rate
```

---

# 88. Pipeline Optimization

Use caching for:

```text
Go modules
pnpm store
Docker layers
test dependencies
```

Do not cache:

```text
secrets
production data
mutable deployment state
```

---

# 89. Parallelization

Run independent jobs in parallel:

```text
Go tests
Frontend tests
OpenAPI
Event contracts
Security scans
```

Then:

```text
integration
```

after required builds are ready.

---

# 90. Pipeline Stages

Recommended:

```text
STAGE 1
Lint / Format / Static

STAGE 2
Unit / Contract / Security

STAGE 3
Build

STAGE 4
Integration

STAGE 5
E2E

STAGE 6
Artifact Scan / Sign / SBOM

STAGE 7
Staging Deploy

STAGE 8
Staging Verification

STAGE 9
Production Approval

STAGE 10
Production Deploy

STAGE 11
Post Deploy Verification
```

---

# 91. CI Gate Matrix

| Gate | PR | Main | Staging | Production |
|---|---:|---:|---:|---:|
| Format | Yes | Yes | N/A | N/A |
| Lint | Yes | Yes | N/A | N/A |
| Unit | Yes | Yes | N/A | N/A |
| SAST | Yes | Yes | N/A | N/A |
| Secret scan | Yes | Yes | N/A | N/A |
| Dependency scan | Yes | Yes | Yes | Yes |
| OpenAPI | Yes | Yes | Yes | Yes |
| Event contract | Yes | Yes | Yes | Yes |
| Migration test | Yes | Yes | Yes | Yes |
| Integration | Yes | Yes | Yes | N/A |
| E2E | Optional/required critical | Yes | Yes | Smoke |
| Container scan | N/A | Yes | Yes | Yes |
| Image signing | N/A | Yes | Yes | Yes |
| Deploy | No | Build | Yes | Approval |
| Post-deploy | No | No | Yes | Yes |

---

# 92. CI Branch Protection

Main branch should require:

```text
passing CI
required review
security checks
contract checks
migration checks
```

No direct push except controlled emergency procedure.

---

# 93. Production Approval Matrix

Recommended:

| Change | Approval |
|---|---|
| Frontend UI | Engineering |
| API behavior | Engineering + owner |
| Database migration | Engineering + DB owner |
| Payment | Engineering + Payment owner |
| Inventory | Engineering + Inventory owner |
| Permission | Security/business owner |
| Audit | Security |
| ERP | Integration owner |
| Critical security | Security |
| Architecture change | Architecture owner |

---

# 94. CI/CD Repository Layout

```text
.github/
└── workflows/
    ├── pr.yml
    ├── ci.yml
    ├── security.yml
    ├── release.yml
    ├── deploy-staging.yml
    └── deploy-production.yml
```

Deployment configuration:

```text
deploy/
├── docker/
├── compose/
├── environments/
└── scripts/
```

---

# 95. Recommended PR Workflow

```text
PR opened
   ↓
change detection
   ↓
parallel checks
   ├── Go
   ├── TypeScript
   ├── OpenAPI
   ├── Events
   ├── DB
   └── Security
   ↓
build
   ↓
integration
   ↓
approval
   ↓
merge
```

---

# 96. Recommended Main Workflow

```text
merge main
   ↓
build immutable artifacts
   ↓
scan
   ↓
sign
   ↓
publish
   ↓
deploy staging
   ↓
E2E
   ↓
approval
```

---

# 97. Recommended Production Workflow

```text
approved release
   ↓
acquire deployment lock
   ↓
verify artifact digest
   ↓
run migration
   ↓
deploy compatible API
   ↓
deploy worker
   ↓
deploy POS web
   ↓
health checks
   ↓
smoke
   ↓
metrics verification
   ↓
release complete
```

---

# 98. Rollback Workflow

```text
incident detected
   ↓
stop rollout
   ↓
evaluate DB compatibility
   ↓
rollback application artifact
   ↓
verify
   ↓
if DB issue:
   forward-fix
   ↓
reconcile
```

Never assume:

```text
application rollback = database rollback
```

---

# 99. Architecture Locks

### LOCK-CICD-001

Production artifacts are immutable.

### LOCK-CICD-002

Production artifacts are built once and promoted.

### LOCK-CICD-003

`latest` is not used as production deployment reference.

### LOCK-CICD-004

OpenAPI breaking changes block CI unless explicitly approved.

### LOCK-CICD-005

Event contract breaking changes require a new event version.

### LOCK-CICD-006

Database migrations are tested before deployment.

### LOCK-CICD-007

Production schema changes use expand/contract where required.

### LOCK-CICD-008

Application rollback does not assume database rollback.

### LOCK-CICD-009

Critical security findings block production release.

### LOCK-CICD-010

RLS and authorization isolation tests are mandatory.

### LOCK-CICD-011

Payment and inventory changes require focused regression tests.

### LOCK-CICD-012

Production deployments are audited.

### LOCK-CICD-013

Concurrent production deployments are prevented.

### LOCK-CICD-014

Secrets are not stored in Git.

### LOCK-CICD-015

Production deployment identities follow least privilege.

### LOCK-CICD-016

Workers must be safe for multiple replicas.

### LOCK-CICD-017

Outbox/event consumers must tolerate rolling deployment compatibility.

### LOCK-CICD-018

Offline sync behavior must be covered by CI regression tests.

### LOCK-CICD-019

Generated API clients are regenerated from the canonical OpenAPI contract.

### LOCK-CICD-020

Release metadata must provide Git SHA and artifact digest traceability.

---

# 100. Final CI/CD Architecture

```text
                    GIT REPOSITORY
                          |
                          v
                    Pull Request
                          |
        +-----------------+-----------------+
        |                 |                 |
      Go CI            Web CI           Contract CI
        |                 |                 |
        +-----------------+-----------------+
                          |
                    Security CI
                          |
                    DB Migration CI
                          |
                          v
                    Build Artifacts
                          |
               +----------+----------+
               |          |          |
              API       Worker     POS Web
               |          |          |
               +----------+----------+
                          |
                   Scan / SBOM
                          |
                        Sign
                          |
                    Artifact Registry
                          |
                          v
                       STAGING
                          |
              +-----------+-----------+
              |                       |
            E2E                    Security
              |                       |
              +-----------+-----------+
                          |
                      Approval
                          |
                          v
                     PRODUCTION
                          |
             +------------+------------+
             |            |            |
          Migration      API         Worker
                                      |
                                    POS Web
                          |
                     Verification
                          |
                    Observability
                          |
                Rollback / Forward Fix
```

---

# 101. Final Principle

The CI/CD system is part of the architecture, not merely deployment automation.

It must continuously enforce:

```text
CODE
 ↓
TEST
 ↓
CONTRACT
 ↓
SECURITY
 ↓
MIGRATION
 ↓
BUILD
 ↓
SIGN
 ↓
STAGING
 ↓
VERIFY
 ↓
APPROVE
 ↓
PRODUCTION
 ↓
OBSERVE
```

This ensures that the implementation cannot silently drift away from the architecture contracts established by the POS platform design.
