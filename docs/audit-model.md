# Audit model

Elevarq Signals emits structured slog records keyed by
`audit_event=<name>` for every operationally significant lifecycle
moment — startup, collection cycles, on-demand requests, exports.
This document catalogues the events, defines the correlation rules
between them, and documents the audit-completeness invariant. Specs:
SIGNALS-R078, R082 (Phase 1 + Phase 2), R083.

## Event catalogue

### Startup events (emitted once at process start)

| Event | Carries |
|---|---|
| `config_validated` | `status` (`ok` / `error`), `warnings` count, `hard_errors` count |
| `high_sensitivity_collectors` | `enabled` (boolean — R075 gate state) |
| `targets_loaded` | `enabled` count, `disabled` count |
| `mode_configured` | `mode` (`standalone` / `managed`), `control_plane_token_configured` (boolean) |

The `mode_configured` event records the audit posture for Mode B.
The token VALUE is never logged — only the configured/not-configured
boolean. The R078 audit-attribute denylist would normally filter the
key (`token` substring match), but a small hand-curated allow-list
permits this specific metadata key.

### Per-target collection events (emitted per cycle)

| Event | Carries |
|---|---|
| `collection_started` | `target`, plus `request_id` and `actor` when correlated to an on-demand request |
| `collection_completed` | `target`, `snapshot_id`, `status` (`success` / `partial` / `failed`), `duration_ms`, `collectors_total` / `_success` / `_failed` / `_skipped`, plus `request_id` and `actor` when correlated |

Interval-driven cycles (the periodic ticker) carry **no**
`request_id` and **no** `actor` — those attributes are omitted
entirely rather than emitted as empty strings.

### `/collect/now` events

| Event | When emitted | Carries |
|---|---|---|
| `collect_now_requested` | request was accepted; cycle queued | `actor`, `request_id`, `requested_targets` (array or `"all_enabled"`), `accepted_targets`, optional `reason` |
| `collect_now_rejected` | request failed validation | `actor`, `error` (one of `invalid_json`, `invalid_request_id`, `invalid_reason`, `empty_targets_array`, `targets_not_collectible`), plus the parsed input fields if available before the rejection |
| `collect_now_dropped` | accepted by validation but cycle won't run | `actor`, `request_id`, `reason_category` |

`collect_now_dropped` has two `reason_category` values:

- `previous_request_pending` — the channel buffer was already full
  at the API handler. The new request never reached the collector
  goroutine.
- `cycle_overlap_skipped` — the collector goroutine picked up the
  request but `running.TryLock` failed because a previous cycle
  (manual or interval-driven) was still in flight.

Either way, the cycle for that `request_id` does **not** run. Both
sub-cases emit the same event name so SIEM filters can grep
`collect_now_dropped` once.

### Export events

| Event | When emitted | Carries |
|---|---|---|
| `export_requested` | client called `GET /export` | `actor`, `source_ip`, `target_id`, `since`, `until` |
| `export_completed` | export finished (success or failure) | `actor`, `status`, `duration_ms`, `size_bytes`, optional `error_category` |

Failure paths set `status=failed` and `error_category` to one of
`invalid_target_id`, `invalid_time_format`, `invalid_time_range`,
`conflicting_selectors`, `snapshot_not_found`, `builder_error`,
or `write_error`.

### Circuit-breaker events (R097)

Operator-safety surfaces. State is in-memory; the audit trail is
the only durable record across restart.

| Event | When emitted | Carries |
|---|---|---|
| `circuit_opened` | Auto: `fail_threshold` consecutive cycle errors on this target | `target`, `from` |
| `circuit_closed` | Auto: `open_cooldown` elapsed after `circuit_opened` | `target` |
| `circuit_paused` | Manual: `POST /collect/pause` or `signalsctl collect pause` | `target`, `from`, optional `actor`, optional `reason` (≤ 256 chars) |
| `circuit_resumed` | Manual: `POST /collect/resume` or `signalsctl collect resume` | `target`, optional `actor` |
| `circuit_pause_noop` | Operator paused an unknown target (permissive no-op) | `target`, `actor`, `reason_category=unknown_target` |
| `collection_skipped` | A non-`closed` circuit prevents a cycle from running | `target`, `reason_category` ∈ `{circuit_open, circuit_paused, min_interval_not_elapsed}` |

`collection_skipped` is shared with R091 (min-snapshot-interval).
The `reason_category` field disambiguates.

### Configuration-reload events (R100)

| Event | When emitted | Carries |
|---|---|---|
| `config_reload_requested` | SIGHUP delivered, or `POST /reload` received | `actor`, `trigger` ∈ `{SIGHUP, http}` |
| `config_reload_applied` | Reload validated and target list swapped | `actor`, `target_count` |
| `config_reload_rejected` | Load or `ValidateStrict` failed | `actor`, `reason` ∈ `{load_failed, validate_failed}`, redacted `error` (capped at 512 chars) |
| `config_reload_target_added` | Reload introduced a target not in the previous list | `target` |
| `config_reload_target_removed` | Reload removed a target from the previous list | `target` |
| `config_reload_target_modified` | A target's connection params changed (pool torn down) | `target` |

`config_reload_rejected` `error` is passed through
`collector.RedactDSN` and length-capped before logging so a
YAML-parse error that quotes config-file content does not leak
credential material into the audit stream.

## Correlation: request_id

`request_id` is the end-to-end correlation key for everything that
happens because of one `/collect/now` call.

- The caller may supply it (regex `^[A-Za-z0-9_-]{1,32}$`).
- If absent, Elevarq Signals generates a ULID (which satisfies the
  regex by construction).
- The 202 response echoes whichever value was used.
- The same value appears on `collect_now_requested`,
  `collect_now_dropped` (when the cycle is dropped), and on every
  per-target `collection_started` / `collection_completed` for the
  cycle when it runs.

A single request thus produces a deterministic sequence of audit
records, all greppable by `request_id=<value>`.

### Example: successful narrowing run, two targets

```
audit_event=collect_now_requested actor=local_operator request_id=01J5K… requested_targets=[prod-main,prod-reporting] accepted_targets=[prod-main,prod-reporting] reason=automated
audit_event=collection_started     target=prod-main      request_id=01J5K… actor=local_operator
audit_event=collection_started     target=prod-reporting request_id=01J5K… actor=local_operator
audit_event=collection_completed   target=prod-main      request_id=01J5K… actor=local_operator status=success duration_ms=312 collectors_total=55 collectors_success=51 collectors_failed=0 collectors_skipped=4
audit_event=collection_completed   target=prod-reporting request_id=01J5K… actor=local_operator status=success duration_ms=298 collectors_total=55 collectors_success=51 collectors_failed=0 collectors_skipped=4
```

### Example: rejected request

```
audit_event=collect_now_rejected actor=local_operator request_id=01J5K… error=targets_not_collectible requested_targets=[prod-main,does-not-exist] accepted_targets=[prod-main] rejected_targets=[{name=does-not-exist,reason=unknown_target}]
```

(no subsequent `collection_started` or `collection_completed` for
that request_id)

### Example: dropped because previous cycle still running

```
audit_event=collect_now_requested actor=local_operator request_id=01J5K…
audit_event=collect_now_dropped   actor=local_operator request_id=01J5K… reason_category=cycle_overlap_skipped
```

## The `actor` field

`actor` identifies the principal that originated a request. The
value is sourced from the bearer token the auth middleware matched —
**never inferred from request shape**.

| Value | Origin |
|---|---|
| `local_operator` | The local API token (`api.token`) matched. |
| `control_plane` | The Elevarq control-plane token matched (only valid in `mode=managed`). |

Events that carry `actor`:

- `collect_now_requested` / `_rejected` / `_dropped`
- `collection_started` / `collection_completed` (when correlated by
  a `request_id`; omitted on interval-driven cycles)
- `export_requested` / `export_completed`

The `mode_configured` startup event does NOT carry `actor` — there
is no caller at startup. The pre-R083 audit events
(`config_validated`, `high_sensitivity_collectors`,
`targets_loaded`) likewise do not carry `actor`.

See [`authentication.md`](./authentication.md) for the
token-to-actor mapping and the auth middleware behaviour.

## Audit completeness invariant (R082)

Every accepted `/collect/now` request reaches a **terminal outcome**
for its `request_id` along exactly one of three branches:

| Branch | Terminal records | When |
|---|---|---|
| **rejected** | one `collect_now_rejected` | validation failed; cycle never queued |
| **dropped** | one `collect_now_dropped` (after the matching `collect_now_requested`) | queued but cycle never ran — channel full at the API handler, or cycle overlap detected by the collector |
| **ran** | one `collection_started` **per target** + one `collection_completed` **per target** | cycle ran |

The "ran" branch is per-target by design. A request that narrows to
two targets emits two `collection_started` records and two
`collection_completed` records, all sharing the same `request_id`.
A request that omits the `targets` field collects every enabled
target and emits one started / completed pair per target. There is
no aggregate "cycle complete" record — keeping the per-target pairs
as the cycle's terminal records means a partial multi-target run
still produces complete per-target audit history for the targets
that ran before any abort.

There is no silent loss path. If a `request_id` appears on
`collect_now_requested` but the audit log shows no records on any
of the three branches above, that's a bug — please open an issue.

The R083 stabilization pass closed the last known hole: a request
that survived validation, queued cleanly, but then got picked up
when a previous cycle was mid-flight (the `running.TryLock` overlap
path). The collector now emits
`collect_now_dropped reason_category=cycle_overlap_skipped` for
that case so the request_id always reaches a terminal record.

## Secret-handling guarantees (R078)

Every audit attribute is filtered through a centralised denylist
before being emitted. The filter rejects keys whose lowercased name
contains any of:

`password`, `secret`, `api_token`, `token`, `dsn`,
`connection_string`, `payload`, `query_result`

A small hand-curated allow-list overrides the substring match for
specific keys that contain a denylist substring but only carry
metadata about a configured value (booleans / counts /
fingerprints), never the secret itself. As of R083 the allow-list
contains exactly one entry:
`control_plane_token_configured` (the boolean on
`mode_configured`).

This filter sits in `internal/safety/audit.go` and applies to every
`safety.AuditLog(...)` call. Individual call sites cannot bypass it
without editing the package. Test
`TestAuditLogDropsForbiddenAttributes` (in
`internal/safety/audit_test.go`) and the end-to-end
`TestCollectNowAuditContainsNoSecrets` /
`TestR083AuditLogsContainNoTokenValue` tests pin this contract.

## Output formats

`signals.log_json: true` switches the slog handler from text to
JSON. The same audit events are emitted; only the encoding changes.
Example JSON shape for `collect_now_requested`:

```json
{
  "time": "2026-04-25T20:14:33.512Z",
  "level": "INFO",
  "msg": "audit",
  "audit_event": "collect_now_requested",
  "actor": "local_operator",
  "request_id": "01J5K6T3HW2A4DGCXV5Z6P0M3R",
  "requested_targets": ["prod-main"],
  "accepted_targets": ["prod-main"]
}
```

## What's intentionally not here

- **No `auth_failed` audit events on token mismatch.** The
  existing 401 + R024 per-IP rate limiter is the auth-failure
  surface. Adding explicit per-failure audit records would be
  noisy under bot scanning and is deferred to a future
  audit-completeness pass with its own rate limiting.
- **No token-rotation audit event.** The file re-read on every
  request is silent by design; the audit log on the next request
  after rotation is sufficient observability.
- **No per-`request_id` outcome lookup endpoint.** Auditors grep
  the audit log by `request_id`; an HTTP endpoint that resolves
  request_id → outcome is Phase 4+ work.
