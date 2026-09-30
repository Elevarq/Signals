# Data-Directory Writability Preflight

## Status

ACTIVE

## Purpose

When Signals runs in its shipped non-root posture (uid 10001,
`readOnlyRootFilesystem`) and the `/data` volume is not writable by that
user, startup previously died with a misleading message:

```
signals: open database: enable WAL: unable to open database file
```

The underlying SQLite error is `(14)` = `SQLITE_CANTOPEN`: the database at
`database.path` (`/data/signals.db`) cannot be created because its directory
is not writable. Blaming **WAL** — merely the operation in progress — sends
operators to the wrong place. In the Helm chart `fsGroup: 10001` masks this;
it bites raw `docker run` / ECS deployments with a root-owned or
mis-permissioned bind mount — exactly what customers do
(Elevarq/Signals#473).

This specification defines an **early writability preflight** on the data
directory and a **clearer wrapped open error**, so a non-writable data
directory fails fast at the config-validate phase with the real cause and
the fix.

This is a **behavioral** specification; tests are mandatory (STDD v2).

## Interfaces

### `db.PreflightWritable(path string) error`

| Input | Type | Constraints |
|-------|------|-------------|
| `path` | `string` | the configured `database.path` (SQLite file path) |

| Output | Meaning |
|--------|---------|
| `nil` | `filepath.Dir(path)` exists and is writable by the current uid |
| non-nil error | the directory is missing, not a directory, or not writable |

Called from `cmd/signals` immediately after `config.ValidateStrict` succeeds
and before `db.Open`.

## Behaviors

- **Given** a writable data directory, **when** preflight runs, **then** it
  returns `nil` and startup proceeds.
- **Given** a data directory that does not exist, **when** preflight runs,
  **then** it returns an error naming the missing directory and the fix.
- **Given** a data directory not writable by the current uid, **when**
  preflight runs, **then** it returns an error that says the directory is
  **not writable**, names the **uid**, and does **not** mention WAL.

## Rules

- **RULE-DDW-01** — Writability is probed by creating and removing a temp
  file in `filepath.Dir(path)`, not by inspecting mode bits (unreliable under
  mounts / ACLs / root-owned bind mounts).
- **RULE-DDW-02** — The preflight runs at the config-validate phase, before
  the store is opened or the collector starts.
- **RULE-DDW-03** — When the SQLite open path itself still surfaces
  `unable to open database file`, the wrapped error names the path, the uid,
  and the directory as the cause — never "enable WAL" alone.

## Invariants

- The preflight never creates the database file or the data directory; it
  only probes writability.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Data directory missing | actionable error naming the directory + mount/chown fix; startup aborts |
| Data directory not writable by current uid | actionable error naming the uid + mount/chown fix; startup aborts |

## Constraints

- No new runtime dependencies; standard library only (`os`, `path/filepath`).
