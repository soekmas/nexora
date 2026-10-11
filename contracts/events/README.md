# Event Contracts

Versioned domain-event contracts belong here.

Use the approved Event Catalog in `docs/architecture/12-event-catalog.md` as the authority.

Events are transactional-outbox messages, delivered at least once. Consumers must be idempotent.
