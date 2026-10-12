# Populated vPREV→vNEXT Upgrade Gate — History, Identity, and Incremental State Survive an Upgrade

## Status

ACTIVE

## Issue

Elevarq/Signals#504 (part of #500, P0). Depends on the `/data` durability
issue (#501, `specifications/cloud-data-durability.md`) and the store
schema-version issue (#502, `specifications/store-schema-version.md`). This
is the regression gate backing #502's forward-only guarantee and #501's
persistence guarantees across a *real upgrade*; it is NOT a fresh-install
test and NOT a same-version restart.

## Purpose

Signals keeps all durable state in a single SQLite store at
`/data/signals.db`: instance identity (`meta.instance_id`), collection
history (`snapshots`, `query_runs`, `query_results`, `events`), cadence /
incremental watermarks (the per-collector last-run times derived from
`query_runs` via `GetLastRunTimes`), per-target cycle outcomes, circuit
state, and representative config references (`targets.secret_type` /
`targets.secret_ref` — a *reference*, never a secret value). The store
migrates forward with embedded, transactional, additive migrations recorded
in the `schema_migrations` ledger (`internal/db`, `Migrate` /
`applyMigration`).

#501 made `/data` durable so it survives compute replacement. #502 made the
re-open safe (forward-only, fail-closed on a newer store). But before this
specification, every `store.Migrate()` exercised in the test suite ran
against a **fresh temp DB**, and the only restart-style test
(`signals_circuit_restart_test.go`) is a same-binary restart — not a version
upgrade. There was **no** test proving that a store written by an **earlier**
schema version (vPREV) — one carrying real populated history, identity, and
watermarks — survives being opened and migrated **forward** by the current
binary (vNEXT), with state intact and collection *continuing* from the
watermarks rather than restarting from zero.

This specification defines that missing proof: a **populated vPREV → vNEXT
upgrade regression gate**. It constructs a representative populated store at
an *earlier embedded migration state* (a strict prefix of the current
embedded migration set — the authentic shape of a store last written by an
earlier release), performs the supported upgrade through the real mechanism
(open the vPREV store with the current binary → `Migrate()` forward), and
asserts the #501/#502 persistence + schema guarantees hold across that
upgrade. It is an **additive** gate: fresh-install coverage is retained.

This specification does **not** change the store schema, what Signals
collects, the migration engine, the durability IaC, or the schema-version
guard. It adds the upgrade *proof* over the behavior those specs already
define.

## Scope

In scope:

- A deep Go lifecycle test (`tests/`) that:
  - builds a representative populated store at an **earlier embedded
    migration state** (vPREV) using the real `internal/db` API +
    `ApplyMigrationSQL` to apply only a strict prefix of the embedded
    migrations;
  - populates it with instance identity, collection history (snapshots +
    `query_runs` / `query_results`), cadence / incremental watermarks (so
    "continue vs restart" is observable), per-target cycle outcomes, and
    representative config references (`secret_ref` only — never a real
    secret);
  - performs the supported upgrade through the **real** mechanism:
    `db.Open` the vPREV store with the current binary → `store.Migrate()`
    forward;
  - asserts the before/after invariants (B1–B7 below).
- Structural guards that the supported deploy modes carry the
  populated-upgrade-safe persistence, reusing/extending the #501 durability
  guards (container durable `/data`; Helm PVC durable default; cloud
  durable-volume retain) so the state-path proof maps to each supported mode
  without re-running the deep test per mode.
- Wiring the deep lifecycle test into the CI gate as an **upgrade gate
  alongside** fresh-install (not replacing it). The repo's `test` gate runs
  `go test -count=1 -race ./...`, which collects the new `tests/` test
  automatically; this spec records that the upgrade gate is the deep
  lifecycle test running under that gate, and the structural guards map it to
  each deploy mode.

Out of scope (explicit non-goals, #504):

- Testing every deploy permutation — representative modes only (container
  named-volume / durable mount, Helm PVC, AWS VM/AMI data-volume), covered
  structurally.
- Removing or weakening fresh-install tests.
- Changing the store schema, the migration engine, the schema-version guard,
  or what Signals collects.
- Docs beyond what the test needs (#505 owns the upgrade-test-matrix docs).
- Credential storage: credentials remain external and are **never** written
  into `signals.db` (reaffirms INV-SIGNALS-07 / INV-DUR-04 / INV-SCHEMA-05).

## Definitions

- **vPREV store** — a store whose `schema_migrations` ledger records a
  **strict prefix** of the binary's current embedded migration set (i.e. it
  was last written by an earlier release that shipped fewer migrations). It
  is populated with real history/identity/watermarks representable at that
  earlier schema. It is NOT "newer" (#502 B4), and NOT a fresh/empty store.
- **vNEXT binary** — the current binary, whose embedded migration set is the
  full set returned by `MigrationFilenames()`.
- **Supported upgrade** — opening the vPREV store with the vNEXT binary and
  running `store.Migrate()`, which applies the missing (suffix) migrations
  forward in order (#502 B4), advancing the ledger to the embedded set.
- **Continue vs restart** — after the upgrade, the per-collector watermarks
  (`GetLastRunTimes`) are intact, so the next collection is bounded by the
  prior watermark (incremental) rather than re-collecting from an empty
  store.

## Behaviors

### B1 — vPREV store is a genuine strict-prefix (older) store, not fresh and not newer

Given a populated store built by applying only a strict prefix of the
embedded migration set and populating it with real rows, When its ledger is
inspected before upgrade, Then the ledger records a **strict prefix** of
`MigrationFilenames()` (at least one embedded migration is missing, and no
recorded filename is unknown to the binary) — it is the authentic shape of an
older release's store, distinguishable from both a fresh store (empty/full
ledger with no data) and a newer store (#502 B5).

### B2 — Instance identity survives the upgrade unchanged

Given a vPREV store whose `meta.instance_id` was set before upgrade, When the
vNEXT binary opens it and `Migrate()`s forward, Then `EnsureInstanceID`
returns the **same** `instance_id` — the upgrade does not regenerate identity
and does not initialize a fresh one over the populated store (INV-DUR-02).

### B3 — Collection history is preserved across the upgrade

Given a vPREV store populated with N snapshots and their `query_runs` /
`query_results`, When the upgrade completes, Then the snapshot count,
`query_runs` count, and `query_results` are all preserved (same counts, same
identities) — no history row is dropped, truncated, or recreated by the
upgrade (INV-DUR-02, INV-SCHEMA-02/03).

### B4 — Watermarks survive AND the next collection continues from them

Given a vPREV store whose `query_runs` establish a per-collector last-run
watermark, When the upgrade completes, Then `GetLastRunTimes` returns the
**same** watermark for that collector AND a subsequent collection inserted
after the upgrade is bounded by (strictly after) that watermark — the next
cycle continues incrementally from prior state rather than restarting history
from zero (INV-DUR-02).

### B5 — Schema is migrated to current; the ledger advanced correctly

Given the strict-prefix vPREV ledger, When the upgrade completes, Then the
`schema_migrations` ledger equals the full embedded set (`MigrationFilenames()`),
the previously-missing (suffix) migrations are now recorded, and the
previously-present (prefix) migrations' rows are unchanged (their
`applied_at` is not rewritten) — the forward-only ledger advanced exactly by
the missing migrations and nothing else (INV-SCHEMA-03/04, #502 B4).

### B6 — A subsequent collection / insert succeeds after the upgrade

Given the upgraded store at the current schema, When a new collection cycle
is persisted (`InsertCollectionAtomic`) and a capability introduced by a
suffix migration is used (e.g. `UpsertCircuitState`, added by the circuit-
state migration), Then both succeed — the upgraded store is fully usable at
the current schema, including the tables the upgrade added.

### B7 — No credential/secret is copied into the store; no empty-store init over populated data

Given the upgrade of a populated vPREV store, When it completes, Then (a) no
credential value (DB password, API token, cloud secret) is present in
`signals.db` — targets carry only `secret_type` / `secret_ref` references
(INV-SIGNALS-07 / INV-DUR-04 / INV-SCHEMA-05); and (b) the upgrade did **not**
initialize an empty store over the populated one — identity, history, and
watermarks are the pre-upgrade values, not defaults (reaffirms R-DUR-10's
"never initialize empty over an existing store").

### B8 — Supported deploy modes carry populated-upgrade-safe persistence

Given the supported representative deploy modes (container durable `/data`;
Helm PVC durable default; cloud VM/AMI durable data volume with retain), When
their declarations are inspected, Then each places `/data` on a resource that
survives the upgrade (instance/AMI/pod replacement) so the store-layer proof
(B2–B7) applies to that mode without re-running the deep test per mode
(INV-DUR-01/05). This reuses the #501 durability structural guards.

## Rules

- **R-UPG-01**: The vPREV store MUST be constructed by applying a **strict
  prefix** of the embedded migration set via the real atomic migration path
  (`ApplyMigrationSQL` over `MigrationFilenames()[:k]`, `k < len`), never by
  hand-writing the full schema — so the fixture is the authentic shape of an
  earlier release's store and the upgrade exercises real forward migration.
- **R-UPG-02**: The populated fixture MUST carry, at the vPREV schema: a set
  instance identity (`meta.instance_id`), ≥2 snapshots with their
  `query_runs` (and ≥1 `query_results` payload), cadence watermarks
  observable via `GetLastRunTimes`, ≥1 per-target cycle outcome, and ≥1
  target row whose `secret_type` / `secret_ref` is a **reference** (e.g.
  `aws-rds-iam` / an ARN-shaped ref), never a secret value.
- **R-UPG-03**: The upgrade MUST be performed through the real mechanism —
  `db.Open` on the vPREV store file followed by `store.Migrate()` — not by
  directly executing migration SQL in the assertion path.
- **R-UPG-04**: The test MUST assert B2–B7 after the upgrade: same
  `instance_id`; preserved snapshot / `query_runs` / `query_results` counts
  and identities; preserved watermark; next collection continues (strictly
  after the watermark); ledger advanced to the full embedded set with prefix
  rows unchanged; a post-upgrade `InsertCollectionAtomic` and a suffix-
  migration-introduced capability both succeed; no credential in the store;
  no empty-store init over populated data.
- **R-UPG-05**: The structural mapping to deploy modes MUST reuse/extend the
  #501 durability guards (`tests/data_durability_test.go`) — the test MUST
  NOT re-run the deep lifecycle proof once per cloud. A single additive guard
  MAY assert the container/compose durable-`/data` posture alongside the
  existing Helm-PVC and cloud-volume-retain guards.
- **R-UPG-06**: Fresh-install coverage MUST be retained — this gate is
  additive. The deep lifecycle test MUST NOT modify or remove the existing
  fresh-install / schema-version / durability-continuity tests.
- **R-UPG-07**: The gate MUST run in CI as part of the existing `test` gate
  (`go test -count=1 -race ./...`); no credential, no live PostgreSQL, and no
  cloud resource are required (state semantics are deployment-independent).
- **R-UPG-08**: If a representative built-image / compose-level lifecycle
  check is cheaply feasible with existing CI tooling, ONE MAY be added;
  otherwise the deep Go lifecycle test + the structural deploy-mode guards
  are the equivalent proof, and that choice MUST be documented (see "Test
  pyramid rationale").

## Invariants

- **INV-UPG-01**: A populated store written by an earlier schema version is
  opened and migrated forward by the current binary with instance identity,
  collection history, and incremental watermarks intact; the next collection
  continues incrementally. (The upgrade-time form of INV-DUR-02.)
- **INV-UPG-02**: The upgrade advances the `schema_migrations` ledger
  forward-only by exactly the missing migrations; it never rewrites an
  already-applied migration row, never downgrades, and never initializes an
  empty store over populated data. (Upgrade-time form of INV-SCHEMA-03/04 +
  R-DUR-10.)
- **INV-UPG-03**: The upgrade introduces no credential value into
  `signals.db`; targets persist only by-reference secret identifiers.
  (Reaffirms INV-SIGNALS-07 / INV-DUR-04 / INV-SCHEMA-05.)
- **INV-UPG-04**: The upgrade gate is additive — fresh-install coverage is
  retained and unmodified.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| The upgrade regenerates or loses `instance_id` | Deep lifecycle test fails (B2 / INV-UPG-01). |
| Snapshot / `query_runs` / `query_results` history is lost or recreated by the upgrade | Deep lifecycle test fails (B3 / INV-UPG-01). |
| A watermark is lost or the next collection restarts from zero | Deep lifecycle test fails (B4 / INV-UPG-01). |
| The ledger does not reach the full embedded set, or a prefix row's `applied_at` is rewritten | Deep lifecycle test fails (B5 / INV-UPG-02). |
| A post-upgrade collection or a suffix-migration capability fails | Deep lifecycle test fails (B6). |
| A credential value appears in the store, or the store was re-initialized empty | Deep lifecycle test fails (B7 / INV-UPG-03). |
| A supported deploy mode would place `/data` on a non-durable resource | #501 durability structural guard fails (B8 / INV-DUR-01). |

## Constraints

- The proof is **store-level and deployment-independent**: upgrade state
  semantics live entirely in `internal/db`, so a single deep lifecycle test
  in `tests/` is the primary proof for every deploy mode; the per-mode
  mapping is structural (template-level), matching the #501 constraint that
  IaC cannot be provisioned in CI. No live PostgreSQL or cloud resource is
  required.
- The vPREV fixture requires ≥1 missing (suffix) migration to model a real
  upgrade; the repo currently ships ≥6 embedded migrations, so a strict
  prefix is always available. If only one migration ever existed the test
  would skip (no upgrade to model) — not applicable at the current count.
- CI gates (`gofmt`, `vet`, `test -race`, `lint`, security) remain green.

## Test pyramid rationale

Three layers, deliberately avoiding N redundant expensive cloud e2e runs:

1. **Deep Go lifecycle test (primary proof).** Upgrade state semantics
   (identity / history / watermark continuity / ledger advance / no-credential
   / no-empty-init) are a property of `internal/db` and are
   deployment-independent. One deep test in `tests/` exercises the real store
   `Open → Migrate` forward on a populated vPREV DB and asserts all of
   B1–B7. This is the authoritative proof.
2. **Structural deploy-mode guards.** Because the state proof is
   deployment-independent, each supported mode only needs to carry a durable
   `/data` for the proof to apply. The #501 guards already assert the Helm
   PVC default and the cloud durable-volume retain; this spec adds the
   container/compose durable-`/data` mapping. This maps the one state proof to
   each mode without re-running it per cloud.
3. **CI wiring as an upgrade gate alongside fresh-install.** The deep test
   runs under the existing `test` gate (`go test -race ./...`), so it gates
   every PR and push next to the retained fresh-install tests.

Per R-UPG-08: a built-image/compose lifecycle check is NOT cheaply feasible
here as a *populated-upgrade* check — the built-image smoke harness
(`scripts/smoke-built-image.sh`) is a fresh-install collect→export smoke with
no vPREV-populated-store seed, and building a second image at an earlier
schema purely to exercise migration would duplicate the deployment-independent
Go proof at container cost. The deep Go lifecycle test + the structural
deploy-mode guards are therefore the equivalent proof, and the existing
built-image smoke (fresh install) is retained unchanged.

## NFR overrides

None. Standard Elevarq security/compliance baseline applies. The gate
strengthens the safety model (a green upgrade now *means* history + identity +
incremental state survived and collection resumes) and never weakens an
existing guarantee.

## Rollback / recovery

Per #502: the policy is forward-only; a downgrade is a restore from backup,
never a reverse migration. This gate additionally models the preserved-data
path (B2–B7); the NOT-preserved path (ephemeral `/data`) is the documented
data-loss case already covered by #501's rollback note and the durability
guards (B8) — a non-durable `/data` is fenced as evaluation-only, so a green
upgrade gate never implies recovery of history an ephemeral disk already
discarded.
