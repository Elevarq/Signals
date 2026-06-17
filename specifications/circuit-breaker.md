# Circuit Breaker — Per-Target Backoff + Operator Pause

## Status

DRAFT

## Purpose

When a target is in trouble, the daemon must back off without operator
intervention. When the operator decides Signals itself is the problem
(or wants to stop poking a database during an incident), the daemon
must obey on demand. This spec covers both paths through a single
per-target state machine.

## State machine

Three states, one in-memory state per target:

| State | Meaning | Collection behaviour |
|-------|---------|---------------------|
| `closed` | Normal. | Cycles run as usual. |
| `open` | Auto-disabled after consecutive failures. | Cycles skipped; auto-recovers after a cooldown. |
| `paused` | Manually disabled by an operator. | Cycles skipped; only an operator `resume` clears it. |

State is in-memory only — a daemon restart resets every target to
`closed`. Past pause / resume audit events live in the audit log
(operator can grep journald) so a restart doesn't erase the operator
trail.

### Transitions

**closed → open** when the per-target cycle has failed 3 consecutive
times (default; configurable via `signals.circuit.fail_threshold`).
"Failed" means `collectTarget` returned a non-nil error — the same
signal that today drives the `collect_error` audit event.

**open → closed** automatically when the cooldown elapses (default
5 minutes; configurable via `signals.circuit.open_cooldown`). The
next scheduled cycle runs; if it fails again, the circuit returns
to `open` immediately (the consecutive-fail counter is preserved
across the open-cooldown boundary so a single recovery cycle is
not required to climb from zero).

**closed | open → paused** when the operator issues
`signalsctl collect pause` (or `POST /collect/pause`). Carries a
free-form operator reason (≤ 256 chars). Manual pause takes
absolute priority over the auto state — a paused target stays
paused until manually resumed.

**paused → closed** when the operator issues
`signalsctl collect resume` (or `POST /collect/resume`). Consecutive-
fail counter resets to zero.

### R092 force interaction

`signalsctl collect now --force --target=X` (R092) bypasses **R091
min-snapshot-interval** but does NOT bypass the circuit state. If
the operator wants to override a `paused` or `open` target, the
correct path is `signalsctl collect resume --target=X` followed by
`signalsctl collect now`. Forcing past the circuit would defeat its
purpose.

The audit event for any cycle that ran while the circuit was
non-`closed` (shouldn't happen post-R097 but a future bug might
introduce it) carries `circuit_bypassed=true` so an auditor can
spot it.

## Interfaces

### CLI

```
signalsctl collect pause [--target=<name>] [--reason="..."]
signalsctl collect resume [--target=<name>]
```

`--target` omitted → applies to every enabled target in the daemon's
config. `--reason` defaults to `manual operator pause via signalsctl`.

Output: one line per target reflecting the new state.

### HTTP

```
POST /collect/pause
Content-Type: application/json

{"target": "prod-db", "reason": "Investigating incident #4321"}
```

Body fields:

- `target` (optional) — name from config. Empty / absent → all enabled
  targets.
- `reason` (optional, ≤ 256 chars) — operator memo. Stored with the
  pause event in the audit log.

Response: `200 OK` with body `{"paused": ["prod-db"]}` listing the
targets whose state changed.

```
POST /collect/resume
Content-Type: application/json

{"target": "prod-db"}
```

Response: `200 OK` with body `{"resumed": ["prod-db"]}`.

Both endpoints inherit the daemon's bearer-auth + Mode A / Mode B
actor decoration (R083).

### Status surface

`GET /status` includes a per-target circuit-state field:

```json
{
  "targets": [
    {
      "name": "prod-db",
      "circuit_state": "closed",
      ...
    },
    {
      "name": "staging-db",
      "circuit_state": "paused",
      "circuit_paused_reason": "investigating incident #4321",
      "circuit_paused_at": "<RFC3339>",
      ...
    }
  ]
}
```

`circuit_paused_at` and `circuit_paused_reason` appear only when the
state is `paused`.

### Metrics

`/metrics` (R079) exposes:

```
# HELP signals_circuit_state Per-target circuit state. 1 = active state, 0 = inactive.
# TYPE signals_circuit_state gauge
signals_circuit_state{target="prod-db",state="closed"}    1
signals_circuit_state{target="prod-db",state="open"}      0
signals_circuit_state{target="prod-db",state="paused"}    0
signals_circuit_state{target="staging-db",state="closed"} 0
signals_circuit_state{target="staging-db",state="paused"} 1
```

Operators alert on `signals_circuit_state{state=~"open|paused"} == 1`.

## Invariants

- **INV-CIRC-01**: State is per-target and in-memory only. Daemon
  restart resets every target to `closed`.
- **INV-CIRC-02**: Manual `paused` takes priority over the auto
  state machine. An auto-open target that gets manually paused is
  `paused`, not `open`; manual resume returns it to `closed`, not
  `open`.
- **INV-CIRC-03**: A target whose circuit is non-`closed` writes no
  rows on a skipped cycle (same invariant as R091's INV-SIGNALS-15).
  Audit event `collection_skipped` carries `reason_category`:
  - `circuit_open` for auto-open targets,
  - `circuit_paused` for manually-paused targets.
- **INV-CIRC-04**: Pause / resume operations are atomic from the
  caller's perspective — the API endpoint and CLI subcommand return
  only after the state has flipped. The next tick uses the new
  state.

## Failure Conditions

- **FC-CIRC-01**: `POST /collect/pause` with `reason` > 256 chars
  → `400 Bad Request` with a diagnostic.
- **FC-CIRC-02**: `POST /collect/resume` with an unknown target
  name → `400 Bad Request` listing configured targets. (Pause is
  more permissive: pausing an unknown target is silently a no-op
  — operator may be pausing a target they're about to add to
  config.)
- **FC-CIRC-03**: `signalsctl collect pause` against a daemon whose
  API token is unreachable → standard HTTP-failure path; the
  `signalsctl` exit code is non-zero with the underlying cause.
- **FC-CIRC-04**: Auto-open transition occurs even when the
  collector framework's own retry / overlap protection (R032) is
  active — the circuit is the outer gate.

## Configuration

```yaml
signals:
  circuit:
    fail_threshold: 3        # consecutive failures before closed → open
    open_cooldown: 5m        # duration in open before auto-recovery to closed
```

Both fields have sensible defaults. `fail_threshold` must be ≥ 1.
`open_cooldown` must be > 0. `ValidateStrict` rejects out-of-range
values.

## Audit events

- `circuit_opened` — auto. Attributes: `target`, `consecutive_fails`.
- `circuit_closed` — auto (cooldown). Attributes: `target`,
  `open_duration_ms`.
- `circuit_paused` — manual. Attributes: `target`, `actor`, `reason`,
  `request_id`.
- `circuit_resumed` — manual. Attributes: `target`, `actor`,
  `request_id`.
- `collection_skipped` (existing) gains two new `reason_category`
  values: `circuit_open` and `circuit_paused`.

## Out of scope

- Explicit half-open / probe state. The simpler "auto-open with
  cooldown" model recovers naturally — when cooldown elapses the
  next scheduled cycle IS the probe. If it fails again, the circuit
  re-opens with no additional probing logic to maintain.
- Persistent pause across restarts. Operator-documented behaviour
  per INV-CIRC-01. A restart effectively says "the operator wants a
  clean slate".
- Per-collector circuits. A target either collects or doesn't —
  granularity below that is the sensitivity-profile concern (issue
  #69).
- Adaptive thresholds based on target health history. The defaults
  (3 consecutive failures, 5min cooldown) are operator-tunable but
  not auto-tuned.

## Analyzer requirements unblocked

None directly. The circuit is operator-side safety; analyzer
ingest is unaffected. Snapshots produced before a circuit opens
still ship; the gap during an open / paused window appears as a
freshness signal (C6, R095) — which is the right operator surface
for "this target's data is stale".
