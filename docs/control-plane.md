# Control plane support

This document describes how `POST /collect/now` accepts an optional
JSON body to narrow the cycle to a subset of configured targets, and
how an optional Elevarq control plane can drive that narrowing
authenticated as a distinct actor. Specs: ARQ-SIGNALS-R082 (Phase 1
+ Phase 2) and R083.

This is operator-facing reference — for the wire contract, see the
spec text in `features/arq-signals/specification.md`.

## Modes

Elevarq Signals runs in one of two modes; the mode is set by
`signals.mode` in the config (default `standalone`).

| Mode | When to use | Auth surface |
|---|---|---|
| `standalone` (default) | Open-source / local-only / community deployments. Operator drives everything via the local API token. | One bearer token (`api.token`). |
| `arq_managed` | Deployments where the commercial Elevarq control plane drives collection. | Two distinct bearer tokens. The local `api.token` for operator commands; a separate `arq_control_plane_token` for the control plane. |

The configured target list in `signals.yaml` is the **authoritative
ceiling** in both modes. The control plane (or any caller) can only
narrow this set, never expand it.

## POST /collect/now — request shape

The endpoint accepts an optional JSON body. **All fields are
optional.** An empty body (or a missing body) keeps the historical
"collect every enabled target" behaviour.

```json
{
  "targets":    ["prod-main", "prod-reporting"],
  "request_id": "abc_123",
  "reason":     "scheduled_arq_cycle",
  "force":      false
}
```

| Field | Validation | Meaning |
|---|---|---|
| `targets` | optional `string[]`; non-empty when present | Subset of configured target names. Absent → collect all enabled targets. Empty array `[]` → 400 (treated as a client bug, never silently coerced). |
| `request_id` | optional `^[A-Za-z0-9_-]{1,32}$` | Correlation identifier propagated through to per-target audit events. When absent, Elevarq Signals generates a ULID. |
| `reason` | optional `^[A-Za-z0-9_-]{1,64}$` | Short tag-style label surfaced in audit events. Not free-form prose. |
| `force` | optional `boolean`, default `false` | Bypasses R091's `min_snapshot_interval` for this cycle only. Does **not** bypass a paused circuit (R097) — resume the target first. |

Validation rejections return `400 Bad Request` with an `error`
field plus `accepted_targets` / `rejected_targets` where applicable.
The cycle is **not triggered** when any target was rejected.

## Examples

The examples below assume `ARQ_SIGNALS_API_TOKEN=dev-local-only-replace-in-prod-32chars` and the
daemon listening on `127.0.0.1:8081` (the default).

### Collect everything (no body)

```bash
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${ARQ_SIGNALS_API_TOKEN}"
```

Response:

```json
{
  "status": "collection triggered",
  "request_id": "01J5K6T3HW2A4DGCXV5Z6P0M3R",
  "accepted_targets": ["prod-main", "prod-reporting", "prod-analytics"]
}
```

`request_id` is daemon-generated when the caller omits one.

### Narrow to a subset

```bash
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${ARQ_SIGNALS_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"targets":["prod-main"]}'
```

Response:

```json
{
  "status": "collection triggered",
  "request_id": "01J5K6T3HW2A4DGCXV5Z6P0M3R",
  "accepted_targets": ["prod-main"]
}
```

### Caller-supplied request_id and reason

```bash
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${ARQ_SIGNALS_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
        "targets":    ["prod-main", "prod-reporting"],
        "request_id": "scheduled_run_2026_04_25",
        "reason":     "automated_cycle"
      }'
```

Response:

```json
{
  "status": "collection triggered",
  "request_id": "scheduled_run_2026_04_25",
  "accepted_targets": ["prod-main", "prod-reporting"]
}
```

### Force-bypass `min_snapshot_interval` (R091)

`force: true` tells the daemon to ignore the per-target
`min_snapshot_interval` guard for this cycle only. Useful when an
operator is iterating on configuration or recovering from an
incident and needs back-to-back snapshots inside the interval.

```bash
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${ARQ_SIGNALS_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"targets":["prod-main"], "force": true}'
```

`force` does **not** bypass a paused circuit (R097). A target
paused via `/collect/pause` stays paused even under `force: true`;
the operator must resume it explicitly. `arqctl collect now
--force` is the CLI surface for this field.

### Invalid request — rejected

```bash
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${ARQ_SIGNALS_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"targets":["prod-main","does-not-exist"]}'
```

Response (HTTP 400):

```json
{
  "error": "one or more targets cannot be collected",
  "accepted_targets": ["prod-main"],
  "rejected_targets": [
    {"name": "does-not-exist", "reason": "unknown_target"}
  ]
}
```

The cycle does **not** run. The audit log contains a
`collect_now_rejected` event correlated by `request_id` (generated
or supplied).

### Invalid `request_id` — rejected

```bash
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${ARQ_SIGNALS_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"request_id":"abc 123"}'
```

Response (HTTP 400):

```json
{ "error": "request_id must match ^[A-Za-z0-9_-]{1,32}$" }
```

### Empty `targets` array — rejected (not silently coerced)

```bash
curl -s -X POST http://127.0.0.1:8081/collect/now \
  -H "Authorization: Bearer ${ARQ_SIGNALS_API_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"targets":[]}'
```

Response (HTTP 400):

```json
{
  "error": "targets must be a non-empty array; omit the field to collect all enabled targets"
}
```

## Audit trace

Every accepted request produces a deterministic sequence of audit
events. The end-to-end correlation key is `request_id`.

The examples below use the slog **text** handler output (the
default). With `signals.log_json: true` the daemon emits the same
events with the same field names, JSON-encoded — see
[`audit-model.md`](./audit-model.md) for an example JSON record.

### Successful run, single target

```
audit_event=collect_now_requested actor=local_operator request_id=01J5K… requested_targets=[prod-main] accepted_targets=[prod-main]
audit_event=collection_started     target=prod-main request_id=01J5K… actor=local_operator
audit_event=collection_completed   target=prod-main request_id=01J5K… actor=local_operator status=success duration_ms=312 collectors_total=55 …
```

### Rejected request

```
audit_event=collect_now_rejected actor=local_operator request_id=01J5K… requested_targets=[prod-main,does-not-exist] accepted_targets=[prod-main] rejected_targets=[{name=does-not-exist,reason=unknown_target}]
```

(no `collection_started` / `collection_completed`)

### Dropped because a previous on-demand cycle is still queued

```
audit_event=collect_now_requested actor=local_operator request_id=01J5K… …
audit_event=collect_now_dropped   actor=local_operator request_id=01J5K… reason_category=previous_request_pending
```

(no `collection_started` / `collection_completed`)

### Dropped because a previous cycle is still running mid-flight

```
audit_event=collect_now_requested actor=local_operator request_id=01J5K… …
audit_event=collect_now_dropped   actor=local_operator request_id=01J5K… reason_category=cycle_overlap_skipped
```

The two `dropped` reasons differ by detection point — channel-full
at the API handler vs. overlap detected inside the collector — but
both mean **the cycle for THIS request_id will not run**. Auditors
can grep by `request_id` to see the drop.

### Audit completeness invariant

Every accepted `/collect/now` request reaches a terminal outcome
for its `request_id` along exactly one of three branches:

- `collect_now_rejected` — validation failure; cycle not queued.
- `collect_now_dropped` — queued but cycle never ran (channel full,
  or cycle overlap).
- **per-target** `collection_started` + `collection_completed` —
  the cycle ran. A multi-target request emits one started /
  completed pair **per target**, all sharing the same
  `request_id`. A request that omits `targets` collects every
  enabled target and emits one pair per target. There is no
  aggregate "cycle complete" record — the per-target pairs are
  the cycle's terminal records.

There is no silent loss path. See
[`audit-model.md`](./audit-model.md) for the full event catalogue
and the per-branch terminal-record rules.

## Authentication mapping

The `actor` field on every audit event is sourced from the matched
bearer token (R083), never inferred from request shape:

| Bearer matched | `actor` | Conditions |
|---|---|---|
| `api.token` | `local_operator` | Any mode. Default for every operator-driven call. |
| `arq_control_plane_token` | `arq_control_plane` | Only when `signals.mode: arq_managed`. |
| Neither | (request never reaches the handler) | 401, R024 rate limiter records the failure. |

See [`authentication.md`](./authentication.md) for the full Mode B
auth model, token rotation, and validation rules.

## Limits and behaviour notes

- **Channel buffer = 1.** Concurrent on-demand requests are
  serialised. The first request is queued; later requests get
  `collect_now_dropped` (channel-full) until the queued one is
  picked up by the collector goroutine.
- **No new rate limiting on accepted requests.** R024's per-IP
  rate limiter on invalid auth attempts continues to apply.
- **Phase 2 actor invariant.** `actor` value derives from the
  matched token. R082 Phase 2 always emitted `local_operator`;
  R083 lifted that to allow `arq_control_plane`.
- **`arq_control_plane` is reserved for `mode=arq_managed`.** In
  `mode=standalone` even a control-plane-token-shaped header value
  is treated as unknown — 401.

## Pause / resume — `POST /collect/pause` and `POST /collect/resume`

Operator override for the per-target circuit breaker (R097). State
is in-memory; daemon restart resumes all targets. Audit log
preserves the pause/resume trail across restart.

### Request shapes

```http
POST /collect/pause
Content-Type: application/json
Authorization: Bearer $ARQ_SIGNALS_API_TOKEN

{"target": "prod-db", "reason": "investigating incident #4321"}
```

- `target` (optional) — name from config. Empty / absent ⇒ apply
  to every enabled target.
- `reason` (optional, ≤ 256 chars) — operator memo carried on the
  `circuit_paused` audit event.

```http
POST /collect/resume
Content-Type: application/json
Authorization: Bearer $ARQ_SIGNALS_API_TOKEN

{"target": "prod-db"}
```

`target` empty ⇒ resume all enabled targets.

### Outcomes

| Path | Status | Body |
|------|--------|------|
| Pause known target | 200 | `{"paused":["prod-db"]}` |
| Pause unknown target | 200 + `circuit_pause_noop` audit event | `{"paused":["new-target-not-yet-in-config"]}` — pause is permissive (operators may pause a target they're about to add) |
| Pause with `reason` > 256 chars | 400 | `{"error":"reason exceeds 256 chars"}` |
| Resume known target | 200 | `{"resumed":["prod-db"]}` |
| Resume unknown target | 400 + available-target list | `{"error":"unknown target ...","available":[...]}` (FC-CIRC-02) |

### Auto-circuit vs manual pause

A target that fails `fail_threshold` consecutive cycles (default 3)
auto-transitions to `open` and auto-recovers after `open_cooldown`
(default 5 min). Manual `paused` takes priority — a paused target
stays paused until `resume`. Pause/resume operations carry the
operator actor and reason directly on the canonical
`circuit_paused` / `circuit_resumed` audit events (one event per
state change, no supplemental `_request` events).

`arqctl collect now --force` bypasses R091's min-snapshot-interval
only; it does NOT bypass a paused circuit. To override a paused
target, resume it first.

## Reload — `POST /reload` and `SIGHUP`

R100: re-read the config file, validate, swap the runtime-mutable
subset (target list) in place. Pools for removed and connection-
modified targets are closed so the next cycle re-dials with new
parameters.

### Triggers

```bash
# Signal
kill -HUP $(pidof arq-signals)

# HTTP
curl -X POST http://localhost:8081/reload \
  -H "Authorization: Bearer $ARQ_SIGNALS_API_TOKEN"
```

### v1 scope

- Add / remove targets.
- Modify a target's connection params or `collectors.profile`
  (R098 sensitivity profile).

### Out of v1 scope (set-at-construction; require daemon restart)

- `poll_interval`, `target_timeout`, `query_timeout`,
  `min_snapshot_interval`.
- `signals.retention.*` (R099) thresholds.
- `signals.circuit.*` (R097) thresholds.

### Outcomes

| Path | Status | Audit |
|------|--------|-------|
| Reload, validation passes | 200 + `{"reloaded":true,"target_count":N}` | `config_reload_requested` → `config_reload_applied` + per-target `config_reload_target_added` / `_removed` / `_modified` |
| Reload, file unreadable | 400 + redacted load error | `config_reload_rejected` with `reason=load_failed` |
| Reload, `ValidateStrict` fails | 400 + redacted validation error | `config_reload_rejected` with `reason=validate_failed` |

A reload that fails to load or validate leaves the running state
untouched. Error messages on both paths are passed through
`collector.RedactDSN` and capped at 512 chars before they reach
the audit stream or the HTTP body (issue #87 hardening).
