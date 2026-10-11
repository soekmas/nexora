---
author:
- Retail Business Platform
date: 2026-10-06
title: NEXORA --- Product Identity & Naming Convention v1
---

# 1. Product Identity

Product name:

**NEXORA**

Product descriptor:

**Retail Business Platform**

Recommended product positioning:

> NEXORA is a modern enterprise retail business platform for sales,
> inventory, payments, promotions, purchasing, customer management,
> finance, workflow, integration, and reporting.

NEXORA is not positioned as a POS-only product.

# 2. Product Hierarchy

``` text
NEXORA
│
├── NEXORA POS
├── NEXORA Sales
├── NEXORA Inventory
├── NEXORA Purchasing
├── NEXORA Customer
├── NEXORA Promotion
├── NEXORA Loyalty
├── NEXORA Finance
├── NEXORA Workflow
├── NEXORA Integration
└── NEXORA Insights
```

The initial implementation focus is NEXORA POS, but the platform
architecture remains broader.

# 3. Naming Rules

## Product

Official product name:

`NEXORA`

Do not write:

-   NexoraPOS as the product name.
-   NEXORA POS Platform when referring to the whole platform.
-   NEXORA ERP unless the product is explicitly positioned as ERP.

Use:

-   NEXORA
-   NEXORA Retail Business Platform
-   NEXORA POS when specifically referring to the POS application.

## Repository

Preferred repository name:

`nexora`

Local workspace:

`nexora/`

Do not create a repository named `nexora` after this identity
lock.

## Backend

Recommended:

`nexora-backend`

or, if repository-level module naming is preferred:

`github.com/<organization>/nexora`

The final public Go module path remains an implementation/deployment
decision and must not be invented until the owning Git organization is
known.

## Applications

``` text
apps/api
apps/worker
apps/pos-web
```

Product-facing names:

-   NEXORA API
-   NEXORA Worker
-   NEXORA POS

Do not rename folders solely for branding if doing so damages
established repository conventions.

## Docker

Preferred image naming pattern:

``` text
nexora/api:<git-sha>
nexora/worker:<git-sha>
nexora/pos-web:<git-sha>
```

The actual registry hostname is environment-specific.

Never use `latest` as the production deployment identity.

## Environment Variables

Use the `NEXORA_` prefix for product-specific environment variables
where practical.

Examples:

``` text
NEXORA_ENV
NEXORA_LOG_LEVEL
NEXORA_DATABASE_URL
NEXORA_REDIS_URL
NEXORA_HTTP_ADDR
```

Existing conventional infrastructure variables may remain unprefixed
when required by libraries or deployment tooling.

Secrets must never be committed.

# 4. API Identity

OpenAPI title:

`NEXORA Retail Business Platform API`

API description should state that the API serves NEXORA platform
capabilities.

Base path remains:

`/api/v1`

Do not change API versioning merely because the product was renamed.

# 5. Event Identity

Domain event names remain domain-based and versioned.

Examples:

``` text
SalesOrderCreated
SalesOrderConfirmed
PaymentCompleted
StockReserved
PromotionApplied
POSSessionOpened
```

Do not prefix every event with `Nexora` unless an external event
namespace specifically requires it.

If an external event namespace is required, prefer:

``` text
nexora.sales.order.created
nexora.payment.completed
nexora.inventory.stock_reserved
```

as an integration naming convention, while preserving internal event
contracts.

# 6. Database Identity

Existing PostgreSQL schema names remain unchanged:

``` text
identity
organization
master
crm
pricing
promotion
sales
pos
payment
inventory
warehouse
purchasing
finance
loyalty
workflow
integration
audit
platform
reporting
```

Do not rename database schemas to include `nexora`.

Database schema names describe domain ownership, not branding.

# 7. Frontend Branding

Application shell:

``` text
NEXORA
Retail Business Platform
```

POS application:

``` text
NEXORA POS
```

Example POS header:

``` text
NEXORA   Store Jakarta 01 · Terminal T03   Rani   ● Online
```

Backoffice navigation:

``` text
NEXORA

Dashboard
Sales
Inventory
Products
Customers
Promotions
Purchasing
Finance
Reports
Administration
```

# 8. UI Tone

NEXORA UI should communicate:

-   professional;
-   enterprise;
-   modern;
-   premium;
-   operational;
-   trustworthy;
-   restrained.

Avoid branding that feels:

-   playful;
-   childish;
-   overly futuristic;
-   AI-themed;
-   overly decorative;
-   generic SaaS.

The existing `POS_Frontend_UI_UX_Spec_v1.md` remains authoritative for
detailed visual rules.

# 9. Repository Documentation

README and architecture documents should identify the product as NEXORA.

Example:

``` text
# NEXORA

NEXORA Retail Business Platform
```

Architecture documents may retain technical artifact names such as:

``` text
POS_Architecture
POS_Database
POS_OpenAPI
```

when the artifact specifically describes POS scope.

Do not mass-rename historical artifact filenames unless there is a
concrete maintenance benefit.

# 10. Branding vs Architecture

Product branding must not alter architecture.

These remain unchanged:

-   modular monolith;
-   PostgreSQL system of record;
-   RLS;
-   tenant isolation;
-   domain boundaries;
-   state machines;
-   permissions;
-   audit;
-   transactional outbox;
-   idempotency;
-   offline sync contract;
-   payment abstraction;
-   ERP adapters.

NEXORA is a product identity, not an architecture change.

# 11. Future Product Expansion

NEXORA may later support:

``` text
NEXORA POS
NEXORA Commerce
NEXORA Inventory
NEXORA Procurement
NEXORA CRM
NEXORA Finance
NEXORA Workforce
NEXORA Analytics
NEXORA Integration
```

The naming system must remain extensible.

# 12. AI Agent Rule

AI coding agents must use NEXORA as the product identity in new
product-facing documentation, UI labels, application metadata, and
generated project descriptions.

Agents must not rename architecture artifacts or database schemas merely
for branding.

If a naming conflict appears, preserve technical compatibility and
report the conflict.

# 13. Naming Decision

This document locks:

``` text
Product:        NEXORA
Descriptor:     Retail Business Platform
Repository:     nexora
POS Product:    NEXORA POS
API:            NEXORA Retail Business Platform API
Workspace:      nexora/
```

Final external Git organization, domain name, package registry, and
trademark availability remain separate decisions and must be verified
before public launch.
