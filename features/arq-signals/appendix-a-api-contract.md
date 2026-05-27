# Appendix A: API Contract

This appendix defines the HTTP API contract for Arq Signals. Any
conforming implementation must expose these endpoints with the specified
behavior.

## Authentication

All endpoints except `GET /health` require a bearer token in the
`Authorization` header:

```
Authorization: Bearer <token>
```

The token can be configured via any of four sources. Precedence
(low → high, later wins): YAML `api.token` → YAML `api.token_file`
→ env `ARQ_SIGNALS_API_TOKEN` → env `ARQ_SIGNALS_API_TOKEN_FILE`.
The two YAML fields are mutually exclusive. If none are supplied,
the system generates a 32-byte random token at startup and logs its
SHA-256 fingerprint (never the value).

Invalid or missing tokens shall return HTTP 401 with a JSON body:
```json
{"error": "missing or invalid Authorization header"}
```

The system shall rate-limit invalid token attempts: after 5 failures
from the same IP within 5 minutes, subsequent requests from that IP
shall receive HTTP 429.

Token comparison must be constant-time to prevent timing attacks.

## Endpoints

### GET /health

**Authentication:** None required
**Response:** HTTP 200
```json
{
  "status": "ok",
  "version": "<semver>"
}
```

### GET /status

**Authentication:** Bearer token required
**Response:** HTTP 200
```json
{
  "instance_id": "<string>",
  "version": "<semver>",
  "target_count": <integer>,
  "targets": [
    {
      "id": <integer>,
      "name": "<string>",
      "host": "<string>",
      "port": <integer>,
      "dbname": "<string>",
      "user": "<string>",
      "sslmode": "<string>",
      "enabled": <boolean>,
      "last_collected": "<RFC3339 timestamp or absent>"
    }
  ],
  "snapshot_count": <integer>,
  "query_catalog_count": <integer>,
  "last_collected": "<RFC3339 timestamp or empty string>"
}
```

**Excluded fields:** The status response must NOT include `secret_type`,
`secret_ref`, passwords, credential paths, or any information that
reveals how or where credentials are stored.

### POST /collect/now

**Authentication:** Bearer token required

Triggers an immediate collection cycle (non-blocking). The actual
collection runs asynchronously.

**Request body:** optional `application/json`. An empty or missing
body keeps the default "collect every enabled target" behaviour.
When present:

```json
{
  "targets":    ["<target_name>", "..."],
  "request_id": "<^[A-Za-z0-9_-]{1,32}$>",
  "reason":     "<^[A-Za-z0-9_-]{1,64}$>",
  "force":      <boolean>
}
```

| Field | Validation | Meaning |
|---|---|---|
| `targets` | optional `string[]`; non-empty when present | Subset of configured target names. Absent → collect every enabled target. Empty array `[]` → 400 (never silently coerced). |
| `request_id` | optional, regex `^[A-Za-z0-9_-]{1,32}$` | Correlation identifier propagated to per-target audit events. Daemon-generated ULID when absent. |
| `reason` | optional, regex `^[A-Za-z0-9_-]{1,64}$` | Short tag-style label surfaced on audit events. Not free-form prose. |
| `force` | optional `boolean`, default `false` | Bypasses R091's `min_snapshot_interval` for this cycle only. Does **not** bypass a paused circuit (R097). |

**Success response:** HTTP 202

```json
{
  "status":           "collection triggered",
  "request_id":       "<echoed or generated ULID>",
  "accepted_targets": ["<target_name>", "..."]
}
```

`accepted_targets` lists every target the cycle will run against
(the supplied filter, or all enabled targets when `targets` was
absent).

**Error responses:**
- `400 Bad Request` — malformed JSON body.
- `400 Bad Request` — `request_id` or `reason` failed regex validation.
- `400 Bad Request` — `targets: []` (empty array).
- `400 Bad Request` — one or more `targets` are unknown or disabled.
  Body shape:
  ```json
  {
    "error": "one or more targets cannot be collected",
    "accepted_targets": ["<target_name>", "..."],
    "rejected_targets": [
      {"name": "<target_name>", "reason": "unknown_target | disabled_target"}
    ]
  }
  ```
  The cycle is **not triggered** when any target was rejected.

### POST /collect/pause

**Authentication:** Bearer token required

Sets a target's per-target circuit-breaker state (R097) to `paused`
with an operator-supplied reason. Pause state is in-memory; daemon
restart resumes all targets, but the pause/resume trail is
preserved in the audit log.

**Request body:** optional `application/json`. Body must not exceed
4096 bytes.

```json
{
  "target": "<target_name, optional>",
  "reason": "<operator memo, optional, ≤ 256 chars>"
}
```

- `target` absent or empty string → pause every enabled target.
- `target` set to a configured target name → pause that target.
- `target` set to an unknown name → permissive no-op: response is
  HTTP 200 echoing the requested name as paused, and a
  `circuit_pause_noop` audit event is emitted. Pause is permissive
  by design so operators can pre-pause a target they're about to
  add to config.

**Success response:** HTTP 200

```json
{ "paused": ["<target_name>", "..."] }
```

**Error responses:**
- `400 Bad Request` — `reason` exceeds 256 characters.
- `400 Bad Request` — malformed JSON body.
- `413 Payload Too Large` — request body exceeds 4096 bytes.

### POST /collect/resume

**Authentication:** Bearer token required

Clears a target's `paused` circuit state, returning it to `closed`.
Symmetrical with pause for the empty-target case; **stricter** for
explicit targets (FC-CIRC-02 — resume must reference a known
target).

**Request body:** optional `application/json`. Body must not
exceed 4096 bytes. Same shape as `/collect/pause`; `reason` is
ignored on resume.

- `target` absent or empty string → resume every enabled target.
- `target` set to a configured target → resume that target.
- `target` set to an unknown name → HTTP 400 (FC-CIRC-02).

**Success response:** HTTP 200

```json
{ "resumed": ["<target_name>", "..."] }
```

**Error responses:**
- `400 Bad Request` — unknown target (FC-CIRC-02). Body shape
  includes the available target list:
  ```json
  {
    "error":     "unknown target \"<name>\"",
    "available": ["<target_name>", "..."]
  }
  ```
- `400 Bad Request` — malformed JSON body.
- `413 Payload Too Large` — request body exceeds 4096 bytes.

### POST /reload

**Authentication:** Bearer token required

Re-reads the configuration file from disk, validates it via
`ValidateStrict`, and (on success) swaps the runtime-mutable
subset of the target list in place (R100). Equivalent to sending
`SIGHUP` to the daemon process.

**v1 scope:** add / remove targets, modify a target's connection
parameters or `collectors.profile`. Out of scope (require daemon
restart): `poll_interval`, `target_timeout`, `query_timeout`,
`min_snapshot_interval`, `signals.retention.*`, `signals.circuit.*`.

**Request body:** none (any body is ignored).

**Success response:** HTTP 200

```json
{
  "reloaded":     true,
  "target_count": <integer>
}
```

**Error responses:**
- `400 Bad Request` — config file unreadable. Body:
  `{"error": "load <path>: <redacted message>"}`.
- `400 Bad Request` — `ValidateStrict` failed. Body:
  `{"error": "validate <path>: <redacted message>"}`.

A failed reload leaves the running state untouched. Both error
paths run their message through `collector.RedactDSN` and cap it
at 512 characters before it reaches the audit stream or the HTTP
body.

### GET /export

**Authentication:** Bearer token required

**Default scope (no selector parameters):** the latest completed
snapshot per active target. See `ARQ-SIGNALS-R084` for rationale.

**Query parameters:**
- `target_id` (optional, integer) — filter to a single target
- `snapshot_id` (optional, string) — return exactly that snapshot.
  Mutually exclusive with `all`. Unknown ID → HTTP 404 (FC-08).
- `all` (optional, `true`/`false`, default `false`) — return every
  snapshot in local storage (the pre-R084 behavior). Mutually
  exclusive with `snapshot_id`.
- `since` (optional, RFC3339) — include data collected after this time
- `until` (optional, RFC3339) — include data collected before this time

When `all`, `snapshot_id`, `since`, and `until` are all absent, the
default R084 scope (latest completed snapshot per target) applies.

**Response:** HTTP 200 with `Content-Type: application/zip`

The response body is a ZIP archive containing:
- `metadata.json`
- `collector_status.json`
- `snapshots.ndjson`
- `query_catalog.json`
- `query_runs.ndjson`
- `query_results.ndjson`

**Error responses:**
- `400 Bad Request` — `snapshot_id` and `all=true` both supplied.
- `404 Not Found` — `snapshot_id` references no row in `snapshots`.

### GET /metrics (optional)

**Authentication:** Bearer token required

Prometheus-format scrape endpoint (R079). **Off by default.**
Enabled when the daemon is started with a non-empty
`signals.metrics.path` and an attached metrics registry. The path
is operator-configurable (default `/metrics` when enabled).

When disabled, the endpoint is not registered and any request
returns the standard ServeMux 404. When enabled, it inherits the
same bearer-token authentication as every other endpoint —
operators that want unauthenticated scraping should bind the API
to loopback and use network-level controls.

**Response:** HTTP 200 with `Content-Type: text/plain; version=0.0.4`
(the Prometheus text-exposition format).

## Export metadata schema

The `metadata.json` file in the export ZIP shall contain:

```json
{
  "schema_version": "arq-snapshot.v1",
  "instance_id": "<string>",
  "collector_version": "<semver>",
  "collector_commit": "<git short hash>",
  "collected_at": "<RFC3339 timestamp>",
  "unsafe_mode": <boolean>,
  "snapshot_count": <integer>,
  "ingest_mode": "analyze" | "history_only",
  "target_identity": {
    "host": "<string>",
    "port": <integer>,
    "dbname": "<string>",
    "username": "<string>"
  }
}
```

`target_identity` is required as of R094 when the export is anchored
to a non-orphan `target_id` (single-target scope, including the R084
default with one active target). It is **omitted** when:

- The export is multi-target (`--all` or the R084 default across N
  active targets) — in that case, per-snapshot `target_identity`
  rows appear inside `snapshots.ndjson` and the top-level block is
  absent.
- The snapshot's `target_id` does not resolve to a row in `targets`
  (the R090 orphan case).

`target_identity` carries **connection identity only** — never
password, secret reference, or `sslmode` (INV-SIGNALS-07).

`snapshot_count` and `ingest_mode` are required as of R086. Their
semantics are:

- **`snapshot_count`** — the number of `snapshots` rows packaged in
  this ZIP. `1` for `--snapshot-id` and the typical R084 single-target
  default; `N` for `--all` (size of the daemon's store) or for the
  R084 default across N active targets.
- **`ingest_mode`** — `"analyze"` for the default `arqctl export`
  (R084 scope) and for the most recent snapshot of an R087 backlog
  burst; `"history_only"` for every other snapshot of an R087 burst.
  Indicates how the consuming Analyzer should process this export
  (advisory; the Analyzer side of the contract is specified in the
  sibling `arq` repository).

When `unsafe_mode` is `true`, the metadata shall also include:

```json
{
  "unsafe_reasons": [
    "<description of bypassed check 1>",
    "<description of bypassed check 2>"
  ]
}
```

The `unsafe_reasons` values shall describe the specific role attributes
that were bypassed (e.g. "role has superuser attribute (rolsuper=true)"),
not generic flags.

## Collector status schema

The `collector_status.json` file in the export ZIP records the
execution outcome of every registered collector for the snapshot
window covered by this export. It is always present (see
INV-SIGNALS-11 / R072 in the main specification). Top-level shape:

```json
{
  "schema_version": "1",
  "target_name": "<string, optional>",
  "collected_at": "<RFC3339 timestamp>",
  "collectors": [
    {
      "id": "<collector ID, e.g. pg_stat_statements_v1>",
      "attempted": <boolean>,
      "status": "success | partial | skipped | failed",
      "reason": "<enum: version_unsupported | extension_missing | config_disabled | execution_error | permission_denied | timeout | savepoint_rollback>",
      "detail": "<human-readable explanation>",
      "row_count": <integer>,
      "duration_ms": <integer>,
      "collected_at": "<RFC3339 timestamp>"
    }
  ]
}
```

`reason` is empty when `status = "success"`. Skipped entries (gated
by version, extension, or config) carry `attempted = false` and the
gating reason. Failed entries (mid-execution faults) carry
`attempted = true` and the runtime reason.

## General conventions

- All responses use `Content-Type: application/json` except `/export`
  which uses `application/zip`.
- All timestamps are RFC3339 in UTC.
- Each response includes an `X-Request-ID` header for tracing.
- The server shall include recovery middleware that returns HTTP 500
  with a JSON error body if an unhandled error occurs.
