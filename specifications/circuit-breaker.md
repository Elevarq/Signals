# Circuit Breaker — Per-Target Backoff + Operator Pause

## Status

ACTIVE

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

State is held in memory and **persisted across restarts** (#455).
Every transition to a non-`closed` state upserts a row into the
`circuit_state` store table; the transition back to `closed` deletes it.
On startup the daemon rehydrates the persisted open/paused states before
the first cycle, so an auto-tripped target is not immediately re-hammered
after a restart and an operator pause is not silently undone. For a
restored `open` circuit the open-cooldown is anchored at the original
transition time, so a cooldown that elapsed during downtime closes on the
first post-restart check. Past pause / resume audit events also live in
the audit log (operator can grep journald).

### Transitions

**closed → open** when the per-target cycle has failed 3 consecutive
times (default; configurable via `signals.circuit.fail_threshold`).
"Failed" means `collectTarget` returned a non-nil error — the same
signal that today drives the `collect_error` audit event.

**open → closed** automatically when the cooldown elapses. The
next scheduled cycle runs; if it fails again, the circuit returns
to `open` immediately (the consecutive-fail counter is preserved
across the open-cooldown boundary so a single recovery cycle is
not required to climb from zero).

The open-cooldown is **not** a fixed interval — it grows on a capped
exponential backoff across consecutive open cycles (R097-B, below), so
a persistently-failing target is re-probed less and less aggressively
rather than hammered every fixed cooldown. The circuit NEVER reaches a
terminal "give up" state: it always eventually re-probes, just at a
longer interval.

### R097-B — capped exponential open-cooldown backoff

**Purpose.** The owner's requirement: "Signals should never stop trying
to collect data, just the interval for the retry must change." A target
that keeps failing must keep being re-probed, but the probe interval
must back off so a chronically-broken target is not retried every fixed
`open_cooldown` forever.

**Inputs.**

- `base` — `signals.circuit.open_cooldown` (default 5m). The cooldown
  for the first open cycle.
- `maxCooldown` — `signals.circuit.max_open_cooldown` (default 1h). The
  cap on the backed-off cooldown. Zero / negative → default. If a
  configured `max_open_cooldown` is smaller than `base`, the effective
  cooldown is clamped to `base` (the cap can never shorten the first
  cooldown below the base).
- `openEpochs` — the count of consecutive `closed → open` transitions
  with no intervening success or manual resume. The first open sets it
  to 1.

**Output — effective cooldown.** For the Nth consecutive open cycle
(`N = openEpochs ≥ 1`):

```
effectiveCooldown(N) = min(base * 2^(N-1), maxCooldown)
```

So with `base=5m`, `maxCooldown=1h`: 5m, 10m, 20m, 40m, 60m, 60m, … The
shift is computed with a guard so `base * 2^(N-1)` cannot overflow
`time.Duration` — once `base << (N-1)` would exceed `maxCooldown` (or
overflow), the result is `maxCooldown`.

**Behaviour.**

- On each `closed → open` transition `openEpochs` increments and
  `openedAt` is set to `now`. The circuit stays open until
  `now - openedAt >= effectiveCooldown(openEpochs)`.
- `open → closed` (cooldown elapsed) preserves `openEpochs` and the
  consecutive-fail counter, so the next cycle IS the probe exactly as
  today; a single failure re-opens, and because `openEpochs` has
  incremented the NEXT open cycle uses the next (larger, capped)
  cooldown.
- **`RecordSuccess` resets the backoff** — `openEpochs → 0` and the
  consecutive-fail counter → 0, so the next open cycle starts again at
  `base`.
- **Manual `Resume` resets the backoff** too (`openEpochs → 0`),
  alongside the existing fail-counter reset.
- **`Pause` does not touch `openEpochs`** — it is an operator state
  orthogonal to the auto-backoff; a resume (not a pause) is what clears
  the backoff.
- **Restore anchor.** A restored `open` circuit anchors `openedAt` at
  the persisted `since` (unchanged) and seeds `openEpochs = 1` — i.e. it
  resumes at the `base` cooldown, not at whatever backed-off level it had
  reached before the restart. Rationale: the pre-restart epoch count is
  not persisted (the `circuit_state` store carries only state + since +
  reason + actor), and starting the restored target at `base` is the
  conservative, re-probe-sooner choice — it never makes a restored target
  wait longer than a fresh open would, and it still backs off again if the
  target keeps failing after the restart.

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

- **INV-CIRC-01**: State is per-target and persisted across restarts
  (#455). A non-`closed` state (open/paused) is rehydrated on daemon
  startup from the `circuit_state` table; the transition back to `closed`
  removes the persisted row. Only open/paused are stored — `closed` is the
  absence of a row.
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
- **INV-CIRC-05** (R097-B): The circuit is never terminal — it ALWAYS
  eventually re-probes. For any finite `openEpochs`, `effectiveCooldown`
  is finite (`≤ max_open_cooldown`), so a persistently-failing target is
  always re-probed after a bounded interval. There is no "give up"
  state.
- **INV-CIRC-06** (R097-B): The effective cooldown is monotonically
  non-decreasing in `openEpochs` and capped: for all `N ≥ 1`,
  `base ≤ effectiveCooldown(N) ≤ max_open_cooldown` (with `base` clamped
  to `max_open_cooldown` when a misconfiguration sets the cap below the
  base). A success or a manual resume resets `openEpochs` to 0, so the
  next open cycle starts again at `base`.

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
    open_cooldown: 5m        # BASE cooldown for the first open cycle
    max_open_cooldown: 1h    # cap on the backed-off cooldown (R097-B)
```

All fields have sensible defaults. `fail_threshold` must be ≥ 1.
`open_cooldown` must be > 0. `max_open_cooldown` must be ≥ 0 (0 → the
1h default). `ValidateStrict` rejects negative values. The effective
cooldown for the Nth consecutive open cycle is
`min(open_cooldown * 2^(N-1), max_open_cooldown)` (R097-B).

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
- Per-collector circuits. A target either collects or doesn't —
  granularity below that is the sensitivity-profile concern (issue
  #69).
- Adaptive thresholds based on target health history. The defaults
  (3 consecutive failures, 5min base cooldown) are operator-tunable
  but not auto-tuned. The backoff in R097-B is a deterministic capped
  exponential of the open-cycle count — it is NOT health-history-driven
  adaptive tuning of `fail_threshold`.

## Acceptance cases (R097-B — backoff)

Language-neutral acceptance cases for the capped-exponential open-cooldown
backoff. Each maps to a test in `internal/circuit/circuit_test.go`.

- **TC-CIRC-09 (normal — backoff grows):** With `base=1m`,
  `max_open_cooldown=10m`, open the circuit; the first open cooldown is
  `1m`. After it elapses and a single failure re-opens, the second open
  cooldown is `2m`, the third `4m`, the fourth `8m` — each consecutive
  open cycle doubles the required cooldown.
- **TC-CIRC-10 (boundary — cap):** With `base=1m`, `max_open_cooldown=3m`,
  the cooldown sequence is `1m, 2m, 3m, 3m, …` — it never exceeds the cap
  and never overflows regardless of how many consecutive opens occur
  (e.g. 60 consecutive opens still cap at `3m`).
- **TC-CIRC-11 (reset on success):** After the backoff has grown (e.g. to
  the 4m level), a `RecordSuccess` resets `openEpochs`; the next open
  cycle's cooldown is back to `base`.
- **TC-CIRC-12 (reset on resume):** A manual `Resume` resets the backoff
  the same way `RecordSuccess` does; the next open cycle is `base`.
- **TC-CIRC-13 (never terminal):** No matter how many consecutive opens
  occur, `ShouldCollect` always transitions `open → closed` once the
  (capped) cooldown elapses — the target is always re-probed.
- **TC-CIRC-14 (pause leaves backoff intact):** `Pause` does not reset
  `openEpochs`; only a `Resume` (or a success) does. (Validated via the
  reset-on-resume path — pause is the operator state, resume is the
  reset.)
- **TC-CIRC-15 (restore anchor):** A restored `open` circuit resumes at
  `base` (`openEpochs = 1`), anchored at the persisted `since`.
- **TC-CIRC-16 (cap < base misconfig):** If `max_open_cooldown < base`,
  the effective cooldown is clamped to `base` (the cap cannot shorten the
  first cooldown below the base).

## Analyzer requirements unblocked

None directly. The circuit is operator-side safety; analyzer
ingest is unaffected. Snapshots produced before a circuit opens
still ship; the gap during an open / paused window appears as a
freshness signal (C6, R095) — which is the right operator surface
for "this target's data is stale".
