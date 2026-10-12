# Store Schema Version — Forward-Only, No-Downgrade, Fail-Closed on a Newer Store

## Status

ACTIVE

## Issue

Elevarq/Signals#502 (part of #500, P0). Depends on the `/data` durability
issue (#501, `specifications/cloud-data-durability.md`). Blocks the upgrade
CI harness (#504), which is explicitly out of scope here.

## Purpose

Elevarq Signals keeps all durable state in a single SQLite store at
`/data/signals.db` and migrates it forward with embedded, transactional,
additive migrations recorded in a `schema_migrations` ledger
(`internal/db/db.go`, `Migrate` / `applyMigration`). A durable `/data`
(INV-DUR-01/02 of #501) survives compute replacement, so the same store can
be re-opened by a *different* binary than the one that last wrote it.

That re-open is the hazard this specification closes. The forward path is
sound — a newer binary applies the migrations the store is missing — but the
original `Migrate` only iterates the running binary's **embedded** migration
set and skips the ones already applied. It never inspects the ledger for
migrations it does **not** ship. So an **older** binary opening a **newer**
store (a store migrated by a later release) sees every one of *its own*
migrations already applied, does nothing, and proceeds to read and write a
schema it does not fully understand — silent, undefined behavior against a
store whose shape is ahead of the code.

This specification makes the store's schema identity explicit and defines a
**forward-only, no-downgrade** policy: before any normal store use or
mutation, the binary determines whether the store carries schema state
**newer** than it understands and, if so, **fails closed** with a typed,
recognizable error and operator recovery guidance. It does **not** rip out
the working forward-migration ledger, add downgrade migrations, or change
what Signals stores — only *when the binary must refuse to run*.

## Scope

In scope:

- `internal/db` — the store open/migrate path:
  - the binary's **max known schema identity**, derived from its embedded
    migration set (`internal/db/migrations.go`, `MigrationFilenames`);
  - a newer-store **guard** that runs inside `Migrate` (the single
    startup-time entry point already called by `cmd/signals/main.go` and the
    doctor/tests), **before** any migration is applied or the store is used;
  - a typed, recognizable fail-closed error for the newer-store case;
  - the documented forward-only / no-downgrade policy and the
    backup/restore recovery note.

Out of scope (explicit non-goals, #502):

- **Downgrade migrations.** The policy is forward-only; a "downgrade" is a
  restore from backup, not a reverse migration.
- The upgrade CI harness (#504) and docs beyond what this change requires
  (#505).
- Changing the store schema or *what* Signals collects (reaffirms #501's
  non-goal).
- Credential storage: credentials remain external and are **never** written
  into `signals.db` (reaffirms INV-SIGNALS-07 / INV-DUR-04).

## Definitions

- **Embedded migration set** — the `.sql` files baked into the running binary
  under `internal/db/migrations/` (`MigrationFilenames`). This is the only
  schema the binary knows how to produce and operate against.
- **`schema_migrations` ledger** — the store table recording, by filename,
  which migrations have been applied to *this* store, with a timestamp. It is
  the store's own record of its schema identity.
- **Max known migration** — the lexicographically greatest filename in the
  binary's embedded migration set (filenames are zero-padded, ordinal-prefixed
  — `001_…` … `006_…` — so sort order is migration order). The binary's
  schema ceiling.
- **Newer store** — a store whose `schema_migrations` ledger records at least
  one applied migration filename that is **not** in the binary's embedded set.
  The store was migrated forward by a later binary than the one now opening
  it.
- **Legacy / unversioned store** — a store that has tables but **no**
  `schema_migrations` table, **or** has the table but **no** rows. It predates
  (or is at the very start of) the ledger. It is **not** "newer".
- **Fail closed** — refuse to proceed: return a typed error from `Migrate`
  before any DDL, read, or write against the store; perform no mutation,
  no recreate, no truncate, no downgrade, no discard of the unknown ledger
  rows.

## Behaviors

### B1 — Fresh store migrates forward and is usable

Given an empty database file, When the binary opens it and runs `Migrate`,
Then the `schema_migrations` ledger is created, every embedded migration is
applied in order, and the store is usable. The ledger's recorded filenames
equal the embedded set.

### B2 — Legacy unversioned store is safely adopted

Given a store that has tables but no `schema_migrations` table (or an empty
ledger), When the binary runs `Migrate`, Then the guard does **not** treat it
as "newer" and does **not** fail closed; the normal forward path adopts it
(records/advances the ledger as its migrations are reconciled) and the store
is usable.

### B3 — Current store reopens idempotently

Given a store already migrated to exactly the binary's embedded set, When the
binary runs `Migrate` again, Then no migration is re-applied, the guard
passes (no ledger filename is unknown), and the store is usable — a reopen is
a no-op.

### B4 — Older store requiring forward migration migrates forward

Given a store whose ledger is a **strict prefix** of the binary's embedded set
(an earlier release's store opened by a newer binary), When the binary runs
`Migrate`, Then the guard passes (every recorded filename is known to the
binary), the missing migrations are applied forward in order, and the store
is usable at the binary's schema.

### B5 — Newer store opened by an older binary is refused, with no mutation

Given a store whose ledger records at least one migration filename **not** in
the binary's embedded set (a newer store opened by an older binary), When the
binary runs `Migrate`, Then `Migrate` returns a **typed, recognizable
fail-closed error** that names the store's recorded max migration and the
binary's max known migration and gives operator recovery guidance; **no
migration is applied, nothing is read for normal use, nothing is written,
truncated, recreated, or discarded** — the store is left exactly as found.

### B6 — Migration failure propagates and does not advance the ledger

Given an embedded migration whose DDL fails, When `Migrate` applies it, Then
the error propagates out of `Migrate`, the failing migration's ledger row is
**not** inserted (the existing atomic `applyMigration` transaction rolls both
the DDL and the ledger insert back together), and a subsequent rerun retries
that same migration from a clean state.

### B7 — Interrupted migration is resumable on rerun

Given a `Migrate` run interrupted after some migrations committed and before
others, When the binary runs `Migrate` again, Then the already-committed
migrations are skipped (their ledger rows exist), the not-yet-applied
migrations are applied forward, and the store reaches the binary's schema —
the per-migration transaction makes the sequence crash-safe/resumable.

## Rules

- **R-SCHEMA-01**: The binary MUST expose an explicit **max known schema
  identity** derived from its embedded migration set — the sorted-max of the
  embedded migration filenames (`internal/db/migrations.go`). There is no
  separate hand-maintained version constant that can drift from the embedded
  set; the embedded set is the single source of the binary's schema ceiling.
- **R-SCHEMA-02**: Before applying any migration and before the store is used
  for normal operation, `Migrate` MUST read the store's `schema_migrations`
  ledger and compute the set of recorded filenames. This guard MUST run inside
  the existing startup-time `Migrate` entry point, after the ledger table is
  ensured to exist and before the forward-apply loop.
- **R-SCHEMA-03**: If **any** recorded ledger filename is **not** in the
  binary's embedded migration set, `Migrate` MUST return a typed,
  recognizable fail-closed error (`*SchemaNewerError`, also matchable via
  `errors.Is(err, ErrSchemaNewer)`). The error message MUST name the store's
  recorded max (offending) migration, the binary's max known migration, and
  MUST give operator recovery guidance (run the newer binary, or restore the
  store from a pre-upgrade backup — forward-only, no downgrade).
- **R-SCHEMA-04**: On the newer-store refusal, `Migrate` MUST NOT apply any
  migration, mutate the store, recreate or truncate any table, downgrade, or
  discard the unknown ledger rows. The store MUST be left exactly as found
  (read-only inspection of the ledger only).
- **R-SCHEMA-05**: A legacy/unversioned store (tables but no
  `schema_migrations` table, or an empty ledger) MUST NOT be treated as newer
  and MUST be adopted by the normal forward path. An empty database file MUST
  likewise migrate forward normally.
- **R-SCHEMA-06**: The forward-only chain MUST be preserved: migrations remain
  embedded, applied in sorted order, each in its own transaction that advances
  the ledger row **only** after its DDL fully succeeds (the existing atomic
  `applyMigration`). Migration errors MUST propagate out of `Migrate`
  unchanged in behavior. No downgrade migrations are introduced.
- **R-SCHEMA-07**: The policy — forward-only, no-downgrade, fail-closed on a
  newer store, and backup-before-upgrade / restore-as-downgrade recovery —
  MUST be documented in this specification (and reaffirmed where operators
  read it). The recorded "downgrade" procedure is restore-from-backup, never a
  reverse migration.

## Invariants

- **INV-SCHEMA-01**: Before normal store use or mutation, the binary
  determines whether the store carries schema state newer than it understands;
  if so it fails closed. No code path lets an older binary read or write a
  newer store.
- **INV-SCHEMA-02**: The newer-store refusal is non-destructive: a refused
  store is byte-for-byte as it was opened (no DDL, no row change, no ledger
  edit). Refusal never degrades recoverability.
- **INV-SCHEMA-03**: Schema evolution is forward-only. The store never
  downgrades; there are no reverse migrations. A binary only ever applies
  migrations the store is missing, never removes or rewrites applied ones.
- **INV-SCHEMA-04**: The ledger advances a migration's row only after that
  migration's DDL has fully committed (atomic per-migration transaction), so a
  crash mid-upgrade leaves the store resumable and never in a half-recorded
  state.
- **INV-SCHEMA-05**: This schema-version work introduces no credential value
  into `signals.db` (reaffirms INV-SIGNALS-07 / INV-DUR-04).

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| A store's `schema_migrations` ledger records a filename the binary does not ship (older binary, newer store) | `Migrate` returns `*SchemaNewerError` (`errors.Is(err, ErrSchemaNewer)`) naming stored-max vs binary-max + recovery guidance; no mutation (R-SCHEMA-03/04, INV-SCHEMA-01/02). |
| An embedded migration's DDL fails | The error propagates out of `Migrate`; the failing migration's ledger row is not inserted; rerun retries it (R-SCHEMA-06, INV-SCHEMA-04). |
| `Migrate` is interrupted mid-sequence | Rerun skips committed migrations and applies the rest; no half-recorded state (B7, INV-SCHEMA-04). |
| A legacy/unversioned or empty store is opened | Adopted by the forward path; never treated as newer (R-SCHEMA-05, B2). |

## Constraints

- The guard is a cheap read of the `schema_migrations` ledger plus a set
  membership check against the embedded filenames — it adds one `SELECT` at
  startup and no per-row cost during normal operation.
- Verification is deterministic and store-level: table-driven Go tests in
  `internal/db` construct fresh / legacy / current / older / newer / failing /
  interrupted stores and assert each behavior. The newer-store case seeds a
  ledger row with a filename ahead of the embedded set (e.g.
  `999_from_the_future.sql`) and asserts the typed error plus that the store
  is untouched. No live PostgreSQL or cloud resource is needed.

## NFR overrides

None. Standard Elevarq security/compliance baseline applies. The fail-closed
posture strengthens the safety model (an older binary can no longer silently
operate a newer store); it never weakens an existing safety guarantee.

## Rollback / recovery

The policy is **forward-only; a downgrade is a restore from backup, never a
reverse migration.** Operators SHOULD back up `/data/signals.db` (or snapshot
the durable data resource of #501) **before** upgrading the binary. If a
newer store must run under an older binary, the supported recovery is either
(a) run the newer binary again, or (b) restore the pre-upgrade store backup
and run the older binary against that. The `*SchemaNewerError` message
surfaces exactly this guidance. The durable data resource's provider-native
snapshots (#501 "Backup / snapshot note") are the recommended backup of
record for the upgrade moment.
