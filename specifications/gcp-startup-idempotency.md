# GCP Startup Reconcile Idempotency

## Status

ACTIVE

## Issue

Elevarq/Signals#732 (audit A7b from the code review).

## Purpose

The GCP `metadata_startup_script` runs on every boot and can be re-run. It
unconditionally minted a new operator API token (replacing the stored one) and
then ran `docker run --name signals`. A repeat invocation with the container
still present fails on the name conflict AFTER the token file has already been
replaced — leaving a new token that does not authenticate to the old running
daemon and preventing reliable reboot/recovery. This specification requires the
startup reconcile to be idempotent: reuse the existing token and reconcile the
container by name instead of unconditionally creating it.

## Scope

In scope:

- `deploy/gcp/scripts/reconcile-collector.sh` — the idempotent reconcile logic
  (token reuse + container reconcile), the single source of truth.
- `deploy/gcp/terraform/main.tf` — embed that script verbatim into the startup
  script (via `file()`), passing `IMAGE_URI` / `TOKEN_FILE` by environment.

Out of scope:

- Azure/AWS startup scripts (this defect is GCP-specific per the audit).
- Deliberate token rotation / image upgrade (a separate explicit operation).
- Live cloud reboot qualification (deferred to the smoke automation, #212).

## Interfaces

- `reconcile-collector.sh` inputs (env): `IMAGE_URI` (required), `TOKEN_FILE`
  (default `/root/signals-api-token`), optional `SIGNALS_EXTRA_ENV`.
- Effects: ensures exactly one `signals` container is running and the token file
  holds the token that container authenticates with.

## Behaviors

### B1 — First boot creates and mints

Given no existing container and no token file, When the reconcile runs, Then it
mints a token, writes it to `TOKEN_FILE`, and `docker run`s the container.

### B2 — Re-run / reboot reconciles without re-running

Given a `signals` container already exists, When the reconcile runs, Then it
reconciles by name (`docker start`, a no-op if already up) and does NOT
`docker run` again — no name-conflict failure.

### B3 — Token is reused, not rotated

Given a non-empty `TOKEN_FILE`, When the reconcile runs, Then it reuses that
token (the running container already carries it) and does NOT mint a new one, so
the operator-retrievable token stays synchronized with the running daemon.

### B4 — Durable data preserved

Given the reconcile on re-run, When it reconciles the container, Then the
durable `/data` mount and the bind-mounted config are untouched.

## Rules

- **GSI-R001**: The startup reconcile MUST reuse an existing operator token and
  mint one only on first boot — never rotate the token out from under a running
  daemon on a re-run.
- **GSI-R002**: The reconcile MUST reconcile the container by name (start if
  present, run only if absent); it MUST NOT unconditionally `docker run`.
- **GSI-R003**: The reconcile logic MUST live in a single shared, tested script
  embedded verbatim by the terraform startup script (no divergent inline copy).

## Invariants

- **INV-GSI-01**: After any number of reboots / script re-runs, exactly one
  collector container runs and the stored operator token authenticates to it.
- **INV-GSI-02**: Re-running the startup path never destroys durable data or
  orphans operator access.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Container already exists on re-run | `docker start` (no-op if up); no `docker run`, no name clash (GSI-R002). |
| Token file already present | token reused; no new mint (GSI-R001). |
| Any reconcile step fails | script exits non-zero so the next run retries (set -euo pipefail). |

## Constraints

- Dependency-free beyond bash, docker, openssl; deterministic. The behavioral
  test drives the script with a mock `docker` (no real containers, no cloud).

## NFR overrides

None. Improves reboot/recovery reliability; weakens no guarantee.

## Rollback / recovery

The script + embed are additive. Reverting restores the prior inline
unconditional run. No persisted state change.
