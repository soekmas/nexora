---
author:
- Human-Crafted Enterprise Retail UI
date: 2026-10-06
title: POS Platform --- Frontend UI/UX Specification v1
---

# 1. Purpose

This document defines the visual, interaction, UX, and frontend
architecture standards for the POS Platform.

The primary objective is to create a POS product that feels like a
mature, professionally engineered retail application rather than a
generic AI-generated SaaS dashboard.

The UI must prioritize:

-   operational speed;
-   clarity;
-   information hierarchy;
-   reliability;
-   realistic retail workflows;
-   accessibility;
-   keyboard and touchscreen usability;
-   offline awareness;
-   consistent visual language.

Visual creativity is allowed inside this system, but the system itself
is intentionally restrained.

# 2. Design Positioning

The product should feel like:

**Modern Enterprise Retail Software**

Not:

-   marketing website;
-   consumer mobile app;
-   generic SaaS dashboard;
-   AI assistant interface;
-   Dribbble concept;
-   glassmorphism showcase;
-   excessive data-card dashboard.

Desired character:

-   professional;
-   practical;
-   premium;
-   calm;
-   dense where operationally useful;
-   fast;
-   trustworthy;
-   consistent;
-   slightly understated.

A useful mental model is:

> Software that a real store employee could use for eight hours a day.

# 3. Human-Crafted UI Principles

The UI must feel deliberately designed by a product team that
understands retail operations.

Prefer:

-   functional hierarchy;
-   restrained color;
-   practical controls;
-   contextual actions;
-   realistic operational details;
-   information density;
-   consistent spacing;
-   clear system feedback;
-   predictable interaction;
-   visible business state.

Avoid:

-   decorative UI without operational purpose;
-   excessive cards;
-   excessive rounded surfaces;
-   gradients used as decoration;
-   giant typography;
-   excessive empty space;
-   excessive animation;
-   random colorful icons;
-   generic SaaS dashboard layouts;
-   unnecessary illustrations;
-   invented AI features;
-   "AI-looking" purple/blue gradient themes;
-   excessive glassmorphism;
-   excessive blur;
-   floating UI everywhere;
-   marketing-style hero sections inside operational screens.

# 4. Anti-AI Visual Rules

These are explicit frontend guardrails.

Do NOT automatically generate:

-   gradient backgrounds;
-   purple-to-blue primary gradients;
-   glassmorphism;
-   frosted cards;
-   huge 48--72px headings;
-   every metric inside a separate card;
-   excessive pill-shaped buttons;
-   excessive rounded corners;
-   random emoji as UI icons;
-   decorative 3D illustrations;
-   floating blobs;
-   excessive shadows;
-   excessive hover animations;
-   generic "AI insights" sections;
-   unnecessary sparkles;
-   AI-themed copy.

A component should exist because it helps the user perform an operation.

If a visual element does not improve task completion, comprehension, or
feedback, remove it.

# 5. Product UI Modes

The product has three primary interaction modes.

## 5.1 Cashier POS

Primary objective:

**Sell quickly and accurately.**

Characteristics:

-   high operational speed;
-   keyboard friendly;
-   barcode scanner friendly;
-   touchscreen friendly;
-   minimal navigation;
-   persistent cart;
-   clear total;
-   clear payment action;
-   clear online/offline status.

## 5.2 Supervisor

Primary objective:

**Control store operations.**

Characteristics:

-   approvals;
-   void/refund oversight;
-   session monitoring;
-   sync monitoring;
-   operational exceptions;
-   stock exceptions;
-   payment exceptions.

## 5.3 Backoffice

Primary objective:

**Manage and analyze the business.**

Characteristics:

-   information density;
-   filters;
-   tables;
-   forms;
-   workflows;
-   reports;
-   configuration;
-   audit visibility.

These modes should share the same design system but not necessarily the
same page layout.

# 6. Visual Language

## 6.1 General

Use a neutral, clean interface.

Preferred surface hierarchy:

-   application background;
-   primary surface;
-   secondary surface;
-   border/divider;
-   interactive surface;
-   semantic status.

Avoid creating a separate card for every piece of information.

Use sections, tables, grouped fields, drawers, dialogs, and panels where
they better fit the task.

## 6.2 Border Radius

Recommended baseline:

-   small controls: 6--8px;
-   panels: 8--12px;
-   dialogs: 10--12px.

Do not make every element heavily rounded.

Avoid excessive pill shapes except for statuses, compact tags, and
intentional controls.

## 6.3 Shadows

Use shadows sparingly.

Primary hierarchy should come from:

-   spacing;
-   borders;
-   surface contrast;
-   typography.

Shadows are appropriate for:

-   modal;
-   dropdown;
-   popover;
-   elevated temporary surface.

They should not be the default treatment for every card.

# 7. Color System

The color system should be restrained.

Suggested semantic baseline:

-   Application background: warm/light neutral.
-   Surface: white or near-white.
-   Primary text: dark neutral.
-   Secondary text: muted neutral.
-   Border: light neutral.
-   Primary action: brand-aligned accent.
-   Success: restrained green.
-   Warning: restrained amber.
-   Danger: restrained red.
-   Information: restrained blue.

Do not use gradients as the default brand treatment.

The primary color should be used mainly for:

-   primary CTA;
-   active navigation;
-   selected state;
-   important links;
-   focused business actions.

Do not saturate the entire interface with brand color.

# 8. Typography

Recommended font:

-   Inter or Geist.

Typography hierarchy should be created primarily through:

-   weight;
-   size;
-   spacing;
-   contrast.

Avoid extreme typography.

Recommended operational ranges:

-   page title: approximately 24--32px;
-   section title: approximately 18--22px;
-   body: approximately 14--16px;
-   dense table text: approximately 13--14px;
-   supporting text: approximately 12--13px.

Large typography is reserved for important totals or exceptional states,
not normal navigation.

# 9. Spacing

Use a consistent spacing scale.

Suggested base:

-   4px;
-   8px;
-   12px;
-   16px;
-   20px;
-   24px;
-   32px.

Operational screens should favor compact but breathable spacing.

Do not introduce large empty gaps simply to make a page look "modern".

# 10. Iconography

Use one coherent icon system.

Recommended:

-   Lucide or equivalent outline icon set.

Rules:

-   icons must have a clear semantic purpose;
-   do not mix multiple icon families;
-   avoid decorative icon collections;
-   icons should not replace text when meaning could become ambiguous;
-   destructive actions should use clear labels.

Do not use emoji as primary operational icons.

# 11. Button System

Button hierarchy:

1.  Primary --- one dominant action.
2.  Secondary --- supporting action.
3.  Tertiary/ghost --- low-emphasis action.
4.  Destructive --- dangerous action.

Avoid multiple competing primary buttons in one viewport.

For POS:

-   `PAYMENT` should be visually dominant.
-   `HOLD` should be secondary.
-   `VOID` should be protected and clearly destructive.
-   `DISCOUNT` should not look like a completion action.

# 12. Form Design

Forms should be practical.

Rules:

-   labels remain visible;
-   validation is contextual;
-   required fields are clear;
-   errors appear near the relevant field;
-   keyboard navigation must work;
-   destructive changes require confirmation where appropriate;
-   avoid unnecessary multi-step forms.

For backoffice, prefer grouped sections over long undifferentiated
forms.

# 13. Table Design

Tables are a primary backoffice component.

Rules:

-   compact rows;
-   clear headers;
-   right-align numeric values;
-   consistent date/time formatting;
-   status shown with restrained badges;
-   filters above the table;
-   pagination or virtualization when required;
-   row actions should be contextual.

Example:

  Order       Customer   Store                Amount Status
  ----------- ---------- ------------ -------------- -----------
  SO-260001   John Doe   Jakarta 01     Rp 4.200.000 Completed
  SO-260002   Sari       Jakarta 02     Rp 8.100.000 Pending

Do not convert every table into large visual cards on desktop.

# 14. Status and State Presentation

The backend state machines are authoritative.

The frontend must represent states faithfully.

Examples:

-   `DRAFT`
-   `CONFIRMED`
-   `PROCESSING`
-   `COMPLETED`
-   `CANCELLED`
-   `VOIDED`
-   `UNKNOWN`
-   `RECONCILIATION_REQUIRED`

Never invent frontend states that imply a different business meaning.

Use semantic labels where useful:

-   Pending;
-   Processing;
-   Completed;
-   Failed;
-   Requires Review;
-   Offline;
-   Syncing.

# 15. Cashier POS Layout

The cashier screen should normally be a single primary workspace.

Recommended structure:

``` text
┌──────────────────────────────────────────────────────────────────┐
│ Store / Terminal / Cashier / Session / Online Status            │
├──────────────────────────────┬───────────────────────────────────┤
│ Search / Barcode             │ CART                              │
├──────────────────────────────┤                                   │
│ Categories                   │ Product lines                      │
│                              │                                   │
│ Product discovery            │ Subtotal                          │
│                              │ Discount                          │
│                              │ Tax                               │
│                              │ TOTAL                             │
│                              │                                   │
│                              │ [ PAYMENT ]                       │
└──────────────────────────────┴───────────────────────────────────┘
```

The cart should remain visible during normal selling.

Do not require the cashier to navigate through multiple pages to add a
product.

# 16. Product Search

Product search must support:

-   barcode input;
-   product code;
-   product name;
-   SKU;
-   keyboard;
-   scanner.

Search should be fast.

When a barcode is scanned:

1.  identify product;
2.  validate sellable state;
3.  retrieve applicable price;
4.  evaluate applicable promotion;
5.  update cart;
6.  provide clear feedback.

Avoid decorative animations.

# 17. Product Grid

Product grid is useful when the cashier browses products manually.

Recommended:

-   image only when genuinely useful;
-   product name;
-   short SKU/code;
-   price where appropriate;
-   availability indicator where appropriate.

Do not make product cards excessively large.

The cashier should be able to see several products simultaneously.

# 18. Cart

Cart line should show:

-   product;
-   variant where relevant;
-   quantity;
-   unit price;
-   discount;
-   line total;
-   relevant promotion indicator.

Quantity changes must be immediate and understandable.

Destructive removal should be easy but not accidental.

# 19. Promotion UX

Promotion complexity belongs to the backend.

Cashier should see simple outcomes.

Good:

``` text
Promotion Applied
Buy selected appliances
Discount: -Rp 500.000
```

Avoid exposing:

-   formula IDs;
-   internal criteria IDs;
-   stackability implementation details;
-   promotion engine technical terminology.

A supervisor may inspect promotion details when necessary.

# 20. Customer UX

Customer selection should normally be a side panel, dialog, or inline
search.

The cashier should not lose the cart.

Customer view may show:

-   name;
-   phone;
-   membership;
-   loyalty balance;
-   recent purchases.

Do not overwhelm cashier with CRM data.

# 21. Checkout UX

Checkout should prioritize payment completion.

Example structure:

``` text
PAYMENT

Total
Rp 13.873.000

[CASH] [CARD] [QRIS] [VA] [TRANSFER]

Amount Paid
[ Rp 15.000.000 ]

Change
Rp 1.127.000

[ COMPLETE SALE ]
```

The payment method selection must be obvious.

Payment errors must explain whether the payment state is known or
unknown.

# 22. Payment Error UX

Never use generic:

> Something went wrong.

For ambiguous payment:

``` text
Payment status is currently unknown.

Do not retry the payment yet.

The system is checking the provider status.

[ Check Payment Status ]
```

This directly reflects the payment state machine.

# 23. Offline UX

Offline status should always be visible but not intrusive.

Example:

``` text
● ONLINE
```

or:

``` text
⚠ OFFLINE
Pending sync: 3
```

Offline messaging should use operational language.

Do not expose technical implementation details such as:

-   IndexedDB;
-   queue lease;
-   outbox;
-   payload version;
-   internal retry codes.

## Offline banner

Example:

``` text
OFFLINE MODE

Sales can continue according to store policy.
Transactions will sync when the connection is restored.

Pending: 3
```

# 24. Sync Center

Supervisor UI should provide:

-   connection status;
-   pending count;
-   synced count;
-   failed count;
-   conflict count;
-   last sync time.

Example:

``` text
SYNC CENTER

Connection       Online
Pending          3
Synced           248
Failed           1
Conflict         0

TXN-001   Synced
TXN-002   Synced
TXN-003   Syncing
TXN-004   Requires Review
```

A conflict should provide a clear recovery action.

# 25. Supervisor UI

Supervisor screens should emphasize exceptions.

Useful operational widgets:

-   open session;
-   cash variance;
-   payment exceptions;
-   offline pending;
-   sync failures;
-   inventory conflicts;
-   refund approvals;
-   promotion approvals.

Avoid turning each metric into a decorative card.

Use a combination of:

-   compact summary;
-   tables;
-   exception lists;
-   filters;
-   action queues.

# 26. Backoffice Navigation

Recommended primary navigation:

``` text
Dashboard

Sales
  Orders
  Payments
  Returns / Refunds

Inventory
  Stock
  Reservations
  Transfers
  Adjustments

Products
  Products
  Categories
  Pricing

Customers

Promotions

Purchasing
  Suppliers
  Purchase Orders
  Goods Receipts

Finance

Reports

Administration
  Users
  Roles
  Stores
  Terminals
  Settings

Audit
```

Actual navigation visibility is permission-driven.

# 27. Permission-Aware UI

Frontend permissions are for UX control, not final security.

The server remains authoritative.

If the user lacks:

`sales.void`

the Void action may be hidden or disabled.

But the API must still reject unauthorized requests.

The UI must never assume that hiding a button is security.

# 28. State-Aware UI

Actions should depend on backend state.

Example:

Completed sale:

-   Void may be available only under policy.
-   Refund may be available.
-   Edit is not available.

Cancelled sale:

-   Normal completion action is unavailable.

Payment UNKNOWN:

-   Retry payment should not automatically appear.
-   Reconciliation/status check should appear.

# 29. Dialog and Drawer Rules

Use dialogs for:

-   confirmation;
-   focused actions;
-   payment;
-   small forms;
-   destructive actions.

Use drawers for:

-   contextual details;
-   customer lookup;
-   order details;
-   audit details;
-   sync item details.

Do not stack multiple dialogs unnecessarily.

Avoid modal chains.

# 30. Loading States

Use context-appropriate loading.

Prefer:

-   inline spinner;
-   skeleton for larger content;
-   disabled action during command processing;
-   progress indication for synchronization.

Avoid full-screen loading for small operations.

A POS should remain responsive.

# 31. Empty States

Empty states should be factual and actionable.

Good:

``` text
No transactions found.

Adjust the filters or choose another date.
```

Avoid decorative illustrations unless they provide genuine value.

# 32. Error States

Errors should include:

-   what happened;
-   whether the operation completed;
-   what the user should do next.

For example:

``` text
Stock changed while this sale was being processed.

The transaction was not completed.

[ Review Cart ]
```

# 33. Toast Notifications

Use toasts for:

-   non-critical confirmation;
-   background status;
-   lightweight feedback.

Do not use toasts for:

-   payment ambiguity;
-   destructive business failures;
-   security warnings;
-   important approval decisions.

Important information must remain visible.

# 34. Keyboard Interaction

Cashier POS should support keyboard shortcuts.

Baseline candidates:

-   F2 --- Search.
-   F4 --- Customer.
-   F6 --- Discount.
-   F8 --- Hold.
-   F9 --- Payment.
-   F10 --- Complete.
-   ESC --- Close/cancel current overlay.

Exact shortcuts remain subject to hardware/browser validation.

Keyboard navigation must not conflict with browser/system shortcuts.

# 35. Touch Interaction

Touch targets should be large enough for POS hardware.

Recommended minimum target size:

-   approximately 40--44px for normal controls;
-   larger for primary cashier actions.

Critical buttons should have sufficient spacing to prevent accidental
taps.

# 36. Responsive Behavior

The product has different priorities by viewport.

Desktop POS:

-   primary target;
-   full cart visibility;
-   keyboard/scanner support.

Tablet:

-   compact two-panel layout where feasible;
-   payment and cart remain accessible.

Small screens:

-   prioritize transaction flow;
-   avoid attempting to squeeze the desktop interface into a phone.

Backoffice may use responsive tables and horizontal scrolling where
necessary.

Do not convert every desktop workflow into a mobile card layout
automatically.

# 37. Accessibility

Minimum expectations:

-   keyboard navigation;
-   visible focus;
-   semantic HTML;
-   sufficient contrast;
-   labels for inputs;
-   meaningful button names;
-   accessible dialog behavior;
-   status announcements where appropriate;
-   do not rely on color alone;
-   error text associated with fields.

Accessibility is particularly important for long operational sessions.

# 38. Motion

Motion must be functional.

Allowed:

-   dialog open/close;
-   drawer transition;
-   subtle state feedback;
-   loading indicators.

Avoid:

-   decorative page transitions;
-   excessive hover animation;
-   continuous movement;
-   bouncing controls;
-   animated gradients.

Default animation duration should be short and unobtrusive.

# 39. Frontend Architecture

Recommended:

``` text
apps/pos-web/
└── src/
    ├── app/
    ├── components/
    ├── features/
    │   ├── auth/
    │   ├── terminal/
    │   ├── pos-session/
    │   ├── cart/
    │   ├── checkout/
    │   ├── sales/
    │   ├── payment/
    │   ├── inventory/
    │   ├── customer/
    │   ├── promotion/
    │   ├── receipt/
    │   └── offline-sync/
    ├── lib/
    │   ├── api/
    │   ├── auth/
    │   ├── offline/
    │   ├── permissions/
    │   └── storage/
    ├── hooks/
    ├── stores/
    └── types/
```

Use feature-based organization.

Avoid large global folders such as:

``` text
services/
utils/
helpers/
```

becoming dumping grounds.

# 40. Component Architecture

Layering:

``` text
Design Tokens
      ↓
Primitive Components
      ↓
Business Components
      ↓
Feature Components
      ↓
Pages
```

Examples:

Primitive:

-   Button;
-   Input;
-   Select;
-   Table;
-   Dialog;
-   Drawer;
-   Tabs;
-   Badge;
-   Toast.

Business:

-   ProductCard;
-   CartItem;
-   PaymentMethod;
-   StockIndicator;
-   PromotionBadge;
-   OrderStatus.

Feature:

-   ProductSearch;
-   CartPanel;
-   CheckoutPanel;
-   PaymentDialog;
-   SyncCenter.

Pages:

-   POSScreen;
-   CheckoutScreen;
-   SalesOrderPage;
-   InventoryPage.

# 41. State Management

Separate state categories.

Server state:

-   products;
-   prices;
-   customers;
-   sales;
-   inventory;
-   payment status.

Local UI state:

-   dialogs;
-   selected tab;
-   search text;
-   temporary filters.

POS transaction state:

-   cart;
-   active transaction;
-   local command queue.

Offline persistence:

-   IndexedDB.

Do not duplicate authoritative server state across arbitrary client
stores.

# 42. API Client

Use a generated TypeScript client from the canonical OpenAPI contract
where practical.

Do not manually duplicate API types across every feature.

API errors should map to the standard backend error envelope.

# 43. Offline Client Architecture

Offline UI must be built around the offline contract.

Suggested flow:

``` text
User Action
    ↓
Local transaction state
    ↓
Offline command
    ↓
IndexedDB
    ↓
Sync worker
    ↓
API
    ↓
Server idempotency
    ↓
Authoritative result
    ↓
Local reconciliation
```

The UI must distinguish:

-   local only;
-   queued;
-   syncing;
-   synced;
-   retryable failure;
-   permanent failure;
-   conflict;
-   reconciliation required.

# 44. Receipt UX

Receipt should clearly distinguish:

-   completed;
-   pending synchronization;
-   payment status;
-   order number;
-   terminal;
-   cashier;
-   store.

Never show a receipt that falsely implies a payment or ERP operation is
complete.

Offline receipt may state:

``` text
Transaction recorded locally.
Server synchronization pending.
```

Only when that is actually the state.

# 45. Audit UX

Audit views are primarily for supervisor, auditor, and administrator
roles.

Display:

-   actor;
-   action;
-   object;
-   timestamp;
-   scope;
-   reason;
-   before/after where permitted.

Sensitive fields must be masked.

# 46. Reporting UX

Reports should prioritize:

-   filters;
-   date range;
-   store;
-   terminal;
-   cashier;
-   product;
-   category;
-   payment method;
-   promotion.

Avoid dashboard decoration.

Use charts only when they improve interpretation.

Tables remain first-class.

# 47. Brand Integration

The interface can reflect the MODENA-like premium home-appliance context
through:

-   restrained brand accent;
-   premium photography only where operationally useful;
-   clean product imagery;
-   precise typography;
-   high-quality spacing.

Do not turn the POS into a marketing website.

Product images should support product recognition, not decoration.

# 48. Microcopy

Use concise operational language.

Prefer:

-   Save
-   Confirm
-   Complete Sale
-   Apply Discount
-   Hold
-   Resume
-   Refund
-   Void
-   Review
-   Retry
-   Reconcile

Avoid:

-   "Let's make this happen!"
-   "Awesome!"
-   "You're all set!"
-   "Something magical happened!"
-   unnecessary AI-style conversational copy.

# 49. Notification Language

Notifications must explain operational meaning.

Bad:

> Something went wrong.

Better:

> Payment status is unknown. Check the provider status before retrying.

Bad:

> Yay! Saved!

Better:

> Order saved as Draft.

# 50. Design System Governance

Any new component should answer:

1.  Does an existing component already solve this?
2.  Is the component reusable?
3.  Does it belong to primitive, business, feature, or page level?
4.  Does it fit the visual system?
5.  Does it improve operational usability?
6.  Does it introduce an AI-looking visual pattern?

Avoid one-off visual components without justification.

# 51. Frontend Definition of Done

A frontend feature is DONE when:

-   behavior matches API contract;
-   permissions are respected in UX;
-   server authorization remains authoritative;
-   state machine behavior is represented correctly;
-   loading state exists;
-   empty state exists;
-   error state exists;
-   offline behavior is defined where applicable;
-   keyboard behavior is considered;
-   touch behavior is considered for POS;
-   accessibility baseline passes;
-   visual system is respected;
-   no unnecessary decorative UI is introduced;
-   tests are added;
-   lint/typecheck/build pass.

# 52. Anti-Drift Review Checklist

Before accepting a new page, ask:

-   Does this look like enterprise retail software?
-   Is there unnecessary decoration?
-   Are there too many cards?
-   Are there too many rounded containers?
-   Is there a gradient that does not serve a functional purpose?
-   Is typography unnecessarily large?
-   Is information density appropriate?
-   Is the primary action obvious?
-   Can a cashier operate this quickly?
-   Can the user understand the business state?
-   Does offline state remain clear?
-   Are errors operationally meaningful?
-   Does the page follow backend permissions and states?
-   Did we accidentally create a generic SaaS dashboard?

If several answers are negative, revise the UI.

# 53. Implementation Order

Frontend implementation should follow:

1.  Design tokens.
2.  Primitive components.
3.  Application shell.
4.  Authentication shell.
5.  Terminal/session.
6.  POS cashier workspace.
7.  Product search.
8.  Cart.
9.  Checkout/payment.
10. Receipt.
11. Offline status and sync UI.
12. Supervisor operations.
13. Backoffice navigation.
14. Sales.
15. Inventory.
16. Customer.
17. Promotion.
18. Purchasing.
19. Finance.
20. Reporting.
21. Audit.
22. Accessibility and visual hardening.

# 54. Relationship to Backend Architecture

Frontend must not invent business rules.

The backend remains authoritative for:

-   authorization;
-   state;
-   pricing;
-   promotion;
-   inventory;
-   payment;
-   loyalty;
-   voucher;
-   finance;
-   ERP;
-   audit.

Frontend is responsible for:

-   user interaction;
-   presentation;
-   local workflow state;
-   offline operational queue;
-   feedback;
-   accessibility.

# 55. Architecture Change Protocol

If a frontend requirement appears to conflict with an architecture lock:

STOP.

Document:

-   existing decision;
-   UI requirement;
-   conflict;
-   options;
-   security impact;
-   API impact;
-   offline impact;
-   recommendation.

Do not silently change the backend contract or state model to
accommodate a UI shortcut.

# 56. Final Design Principle

The most important rule:

> The interface should look like a real product built for real retail
> operations, not a showcase of what an AI can generate.

When choosing between:

-   a more decorative UI; and
-   a more practical UI,

choose the practical UI.

When choosing between:

-   a generic modern pattern; and
-   a pattern that reflects the actual POS workflow,

choose the actual POS workflow.

When choosing between:

-   visual novelty; and
-   operational clarity,

choose operational clarity.
