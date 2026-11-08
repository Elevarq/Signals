# Bounded Export Memory and Cancellation

## Status

ACTIVE

## Issue

Elevarq/Signals#730 (audit A6 from the code review). POC/2.0 scope.

## Purpose

Full-history export materializes the selected snapshots, query runs, and decoded
rows, and the HTTP path additionally buffers the whole ZIP in memory before
responding. There was no admission bound on concurrent exports, and the
expensive read path carried no request context, so a cancelled or timed-out
request kept working. Under large history and concurrency this can exceed the
shipped pod memory limit. This specification bounds concurrent exports and
propagates request-context cancellation so excess work is rejected clearly and
a cancelled export frees resources promptly.

This is the enforceable core (admission + cancellation). A load-tested capacity
envelope / streaming of the materialization is a documented follow-up.

## Scope

In scope:

- `max_concurrent_exports` config (default 2; 0 = unlimited), enforced in the
  HTTP `/export` handler as a non-blocking admission bound → HTTP 429 on excess.
- A context-aware `Builder.WriteToCtx(ctx, w, opts)` that checks cancellation at
  each materialization phase boundary; `WriteTo` delegates with a background
  context for non-HTTP callers.
- Chart + config-schema exposure of the new setting.

Out of scope (documented follow-up):

- A representative load/stress harness measuring peak RSS / store wait to
  publish a supported capacity envelope.
- Streaming / paginating the per-row materialization (the admission bound caps
  peak memory in the meantime).
- Row-level `QueryContext` plumbing into every store read (phase-boundary
  cancellation is the shipped granularity).

## Interfaces

- Config `signals.max_concurrent_exports` (int, default 2; <= 0 = unlimited).
- `Builder.WriteToCtx(ctx context.Context, w io.Writer, opts Options) error`.
- HTTP `GET /export`: on admission failure returns `429 Too Many Requests` with
  `Retry-After` and `{"reason":"concurrency_limit"}`; on cancellation returns
  `499` (client closed) or `503` (deadline exceeded), never a partial `200`.

## Behaviors

### B1 — Admission bound rejects excess

Given `max_concurrent_exports = N` and N exports already in flight, When another
`/export` request arrives, Then it is rejected with HTTP 429 (and audited
`export_rejected` reason `concurrency_limit`) rather than queued or buffered.

### B2 — A released slot is reusable

Given an in-flight export completes, When a new request arrives, Then a freed
slot admits it.

### B3 — Unlimited opt-out

Given `max_concurrent_exports <= 0`, When requests arrive, Then all are admitted
(the limiter is nil).

### B4 — Cancellation aborts and frees

Given a request whose context is cancelled (client closed) or exceeds its
deadline, When `WriteToCtx` runs, Then it returns the context error at the next
phase boundary, writes no archive bytes to the response, and releases the
export lock and admission slot.

### B5 — Non-HTTP callers unaffected

Given a caller using `WriteTo` (background context), When it runs, Then it
behaves exactly as before (no cancellation).

## Rules

- **BEX-R001**: The `/export` handler MUST admit at most
  `max_concurrent_exports` concurrent exports and reject excess with HTTP 429,
  without buffering the rejected request's work.
- **BEX-R002**: `max_concurrent_exports <= 0` MUST mean unlimited; the default
  MUST be 2 so peak export memory is bounded by default.
- **BEX-R003**: Export MUST honour request-context cancellation — `WriteToCtx`
  returns the context error at a phase boundary and writes no partial archive;
  `WriteTo` MUST remain a background-context shim for non-HTTP callers.

## Invariants

- **INV-BEX-01**: Peak concurrent exports (and thus peak export materialization
  memory) is bounded by `max_concurrent_exports` unless the operator opts out.
- **INV-BEX-02**: A cancelled export never emits a partial/`200` archive and
  always releases its lock + admission slot.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Concurrent exports at the limit | HTTP 429 + `Retry-After`; audited `export_rejected`/`concurrency_limit` (BEX-R001). |
| Request cancelled mid-export | `WriteToCtx` returns `context.Canceled`; HTTP 499; no partial archive (BEX-R003). |
| Request deadline exceeded | `WriteToCtx` returns `context.DeadlineExceeded`; HTTP 503 (BEX-R003). |

## Constraints

- Dependency-free (buffered channel limiter + `context`); deterministic unit
  tests (limiter admission; cancelled `WriteToCtx`). No PostgreSQL, no network.

## NFR overrides

Caps peak export memory at the cost of rejecting excess concurrency (429). An
operator opting into unlimited (`0`) re-accepts the unbounded risk and must size
the pod memory limit accordingly.

## Rollback / recovery

The limiter and context checks are additive. `max_concurrent_exports = 0`
restores unlimited concurrency; `WriteTo` is unchanged for existing callers.
