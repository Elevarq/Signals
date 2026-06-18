# `signalsctl doctor` — Pre-flight Verification

## Status

DRAFT

## Purpose

A read-only CLI subcommand that runs operator-facing pre-flight
verification on an arq-signals deployment, before the daemon is started
or after a config change. Surfaces problems with exit code 0 (all OK) or
non-zero (one or more checks failed), so the command is shell-friendly
for CI gates and operator runbooks.

Promotes the manually-encoded "before editing arq-signals" feedback
rule into engineering work (issue #43, parent #39).

## Scope

`signalsctl doctor` runs the following checks against the loaded config and
the targets it declares. Each check is independent — one check failing
does not short-circuit the others; all checks run and the union of
findings is reported.

| ID | Name | Description |
|----|------|-------------|
| C1 | config_valid | Config file present, parseable, and `ValidateStrict` returns no error. |
| C2 | store_writable | The configured SQLite store directory exists and is writable by the running user. |
| C3 | target_reachable | For each enabled target, the configured `host:port` accepts a TCP connection within 3 seconds. |
| C4 | role_safe | For each reachable target, `collector.ValidateRoleSafety` confirms the role does not hold superuser / replication / bypassrls (R013, R018–R020). |
| C5 | collector_prerequisites | For each reachable target, classifies every enabled collector as `available` / `extension_missing` / `version_unsupported` / `config_disabled`. Uses the same `pgqueries.GatedIDsByReason` logic the daemon runs at cycle start (EA-R001), surfaced earlier so operators see the gating before deployment. Depends on C3 + C4. |
| C6 | snapshot_freshness | For each enabled target, reads the daemon's local SQLite store and reports staleness against the configured `poll_interval`. OK when freshness < 2× interval; WARN when ≥ 2× interval; FAIL when the target is enabled but has zero completed snapshots and the daemon has been running long enough that one cycle should have completed. Does NOT depend on C1..C4. |

All checks are read-only. The command opens connections but performs no
writes; the only persistence is the doctor's own report (to stdout).

## Inputs

| Flag / arg | Type | Default | Description |
|------------|------|---------|-------------|
| `--config` | path | inherits from `signalsctl` | Config file location. |
| `--json` | bool | `false` | Emit a single JSON object instead of human-readable text. |
| `--check` | string (repeatable) | empty (all checks) | Run only the named checks (e.g. `--check=C1,C3`). Unknown names → exit 2 with diagnostic. |

## Outputs

### Text mode (default)

One line per check, with `OK` / `WARN` / `FAIL` status, the check ID
and name, and a short detail. Failures may add a hint line. Final line
summarises pass/fail counts.

Example:

```
OK   C1 config_valid              loaded /etc/signals/signals.yaml
OK   C2 store_writable            /var/lib/signals
FAIL C3 target_reachable prod-db  dial tcp 10.0.0.7:5432: connect: connection refused
OK   C3 target_reachable staging  reached staging.example.com:5432 in 14ms
WARN C4 role_safe       prod-db   skipped (target_reachable failed)
OK   C4 role_safe       staging   role signals_ro has no unsafe attributes

Summary: 3 OK, 1 WARN, 1 FAIL
```

### JSON mode (`--json`)

```json
{
  "schema_version": "1",
  "generated_at": "<RFC3339 timestamp>",
  "checks": [
    {
      "id": "C1",
      "name": "config_valid",
      "target": "",
      "status": "ok" | "warn" | "fail",
      "detail": "<string>",
      "duration_ms": <integer>
    }
  ],
  "summary": {
    "ok": <integer>,
    "warn": <integer>,
    "fail": <integer>
  }
}
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | All checks passed (no FAIL, no WARN). |
| 1 | One or more checks emitted FAIL. |
| 2 | Usage error (unknown `--check` name, missing config, etc.). |

WARN does not by itself trigger non-zero exit; only FAIL does. A WARN
exists for situations where a downstream check is unrunnable but the
upstream cause is already reported as FAIL — surfacing the dependency
without double-counting.

## Invariants

- **INV-DOC-01**: No writes to PostgreSQL targets. Doctor opens a
  read-only transaction for any target query.
- **INV-DOC-02**: No writes to the SQLite store. The `store_writable`
  check creates and immediately deletes a probe file; no doctor output
  persists to the store.
- **INV-DOC-03**: Credentials never appear in any output (text or JSON),
  including in error messages.
- **INV-DOC-04**: All checks run independently. A failure in one check
  does not skip others — except where a downstream check has an
  upstream dependency (`role_safe` depends on `target_reachable`), in
  which case the downstream emits WARN, not FAIL.

## Failure Conditions

- **FC-DOC-01**: Config file does not exist or cannot be parsed →
  `C1=FAIL`, exit 1. Subsequent target-scoped checks (C2, C3, C4)
  emit WARN with detail "skipped (config_valid failed)".
- **FC-DOC-02**: Store directory does not exist or is not writable →
  `C2=FAIL`. Other checks continue.
- **FC-DOC-03**: TCP dial times out (3s) or refuses → `C3=FAIL` for
  that target. Other targets continue. `role_safe` for the same
  target emits WARN.
- **FC-DOC-04**: Role validation returns an unsafe-role finding →
  `C4=FAIL` for that target. Other targets continue.
- **FC-DOC-05**: `--check` names an unknown check ID → exit 2 with a
  diagnostic listing the supported IDs.
- **FC-DOC-06**: C5 cannot open a connection to the target (any
  pgxpool failure that would surface as C4 FAIL) → C5 emits WARN with
  detail "skipped (target_reachable / role_safe failed)". C5 does
  NOT FAIL on connection problems — C3 / C4 already do.
- **FC-DOC-07**: C6 cannot open the daemon's SQLite store
  (e.g. doctor running before the daemon has ever booted) → C6
  emits WARN with detail "store unreadable: <cause>". This is
  informational, not a FAIL — pre-daemon doctor runs are a valid
  use case.

## Configuration

The doctor command consumes the same config file as the daemon. It
does not introduce a new config surface. The doctor honors:

- `signals.store_path` (for C2)
- `signals.targets` (for C3, C4)
- TLS / sslmode / secret resolution (for C4, mirrors the daemon's
  connection path)

## Sensitivity

Low. Doctor reads the config and probes connectivity; it does not
read user data, schema, or query results. Credentials are resolved
in memory and never appear in output.

## Out of scope

- Workspace-policy / canonical-sibling checks (the original "before
  editing arq-signals" feedback rule). Those are developer-workflow
  concerns better served by a `make preflight` target or a separate
  `scripts/dev-doctor.sh`. Doctor v1 targets operator pre-flight.
- Extension-presence checks beyond what `ValidateRoleSafety` covers.
  The gated-collector framework (EA-R001) already reports
  `extension_missing` after the daemon runs a cycle; doctor's job is
  to confirm reachability + role safety, not to enumerate the future
  collector_status.json.
- Auto-remediation. Doctor reports; the operator fixes.

## Analyzer requirements unblocked

None directly. Doctor is an operator tool, not an analyzer input.
Indirectly, doctor improves the share of healthy daemons in
production, raising the fraction of analyzer-bound exports that
arrive with full collector_status coverage.
