# Database Migrations

Migrations are immutable after shared deployment.

`000001_baseline_hardened.sql` is the active hardened baseline from the approved database design work.

The original baseline is retained under `docs/reference/` for comparison only. It is **not** an active migration and must not be executed after the hardened baseline is selected.

Production schema changes must use expand/contract where required. Application rollback must not depend on reversing production database migrations.
