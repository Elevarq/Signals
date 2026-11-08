# Collector Shutdown Join

## Status

ACTIVE

## Issue

Elevarq/Signals#731 (audit A7a from the code review).

## Purpose

The daemon started `Collector.Run` in a goroutine with no completion join, then
returned after API shutdown and closed the store via a deferred call. In-flight
collection/delivery could still own the store when `store.Close()` ran, risking
lost work or abrupt cleanup, and the collector goroutine was never joined (a
leak). The server-error path was worse: it returned immediately without even
cancelling the collector. This specification requires every shutdown path to
cancel then join the collector within a bounded deadline before closing its
dependencies.

Atomic persistence (R077) already reduces corruption risk; this issue does not
claim corruption was reproduced. The fix is lifecycle ownership, not a
corruption patch.

## Scope

In scope:

- `cmd/signals` daemon shutdown: signal the collector goroutine's completion,
  cancel its context, and join it (bounded) before the deferred `store.Close`
  on BOTH the signal path and the server-error path.
- A bounded, non-zero shutdown deadline so a misbehaving dependency cannot hang
  exit.

Out of scope:

- Delivery cancellation/deadlines internal to the exporter (F15 / #721) and
  archive-failure propagation (F11 / #717) — coordinated, not re-implemented.
- Changing `Collector.Run`'s own ctx-driven stop behaviour (it already returns
  and closes pools on ctx cancellation).

## Interfaces

- `joinCollector(done <-chan struct{}, timeout time.Duration) bool` — waits up
  to `timeout` for `done` (closed when the collector goroutine returns).
  Returns true if the collector stopped within the deadline, false if the
  deadline elapsed first. The caller MUST cancel the collector's context before
  calling.
- `collectorShutdownTimeout` — the bounded deadline (positive, ≤ 1 minute).

## Behaviors

### B1 — Signal-path shutdown joins the collector

Given a running daemon, When SIGINT/SIGTERM fires (ctx cancelled), Then the
daemon joins the collector goroutine (bounded by `collectorShutdownTimeout`)
before closing the store and returning nil.

### B2 — Server-error path also cancels and joins

Given the API server exits with an error, When the daemon handles it, Then it
cancels the collector context, joins the collector (bounded), closes the store,
and returns the server error — it does NOT return before joining.

### B3 — Finished collector is joined immediately

Given the collector goroutine has already returned, When `joinCollector` runs,
Then it returns true without waiting.

### B4 — Hung collector honours the deadline

Given the collector does not finish, When `joinCollector` runs, Then it returns
false after `timeout` and the daemon proceeds with a logged warning rather than
hanging exit.

## Rules

- **SHUT-R001**: Every shutdown path (signal and server-error) MUST cancel the
  collector's context and join its goroutine before closing the store or
  returning.
- **SHUT-R002**: The join MUST be bounded by a positive, finite deadline
  (`collectorShutdownTimeout`); on deadline elapse the daemon proceeds with a
  warning rather than blocking exit indefinitely.
- **SHUT-R003**: The server-error path MUST NOT return before the cancel+join;
  it returns the error only after the collector is joined and the store closed.

## Invariants

- **INV-SHUT-01**: `store.Close()` never runs while the collector goroutine is
  still running within the deadline — the deferred close is reached only after
  `joinCollector` returns.
- **INV-SHUT-02**: The collector goroutine is always signalled to stop
  (context cancelled) on exit; it is not leaked.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Collector finishes within deadline | `joinCollector` returns true; shutdown proceeds (SHUT-R001). |
| Collector does not finish within deadline | `joinCollector` returns false; daemon logs a warning and proceeds (SHUT-R002). |
| API server error | Collector cancelled + joined, store closed, error returned (SHUT-R003). |

## Constraints

- Deterministic, dependency-free (channels + context + time); no network, no
  PostgreSQL in the unit test.

## NFR overrides

None. Strengthens shutdown correctness and resource hygiene; weakens nothing.

## Rollback / recovery

The join is additive shutdown sequencing. Reverting restores the prior
fire-and-forget behaviour; no persisted state or runtime API changes.
