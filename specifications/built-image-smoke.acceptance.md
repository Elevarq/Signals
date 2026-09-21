# Acceptance Tests: Built-Image Smoke

## Feature

`specifications/built-image-smoke.md`

## Test Cases

### TC-BIS-01: Normal — built image produces a snapshot against real PostgreSQL

**Rule:** BIS-R001, BIS-R002, BIS-R003, BIS-R004 — happy path

**Scenario:** The image built from the current source is run against a
freshly seeded ephemeral PostgreSQL and completes a real cycle.

**Given:**
- A built Signals image loaded locally and referenced via
  `SIGNALS_SMOKE_IMAGE`.
- An ephemeral `postgres:16-alpine` seeded from `examples/init.sql`
  (non-superuser `signals` role + representative schema/data), with
  `pg_stat_statements` preloaded.

**When:**
- `bash scripts/smoke-built-image.sh` is run.

**Then:**
- The collector API `/health` returns success from inside the container.
- `signalsctl collect now --force` exits 0.
- `signalsctl export --output /data/snapshot.zip` exits 0.
- `snapshot.zip` is non-empty and (where `unzip` is available) contains
  `metadata.json` and a `collector_status.json` payload.
- The harness exits 0 and prints `smoke: PASSED`.

---

### TC-BIS-02: Boundary — snapshot must be non-empty and well-formed

**Rule:** BIS-R004 / BIS-B3 — boundary on the artifact of the run

**Scenario:** The cycle runs but the produced snapshot is empty or is
missing the required entries.

**Given:**
- A run that reaches the export step but yields a zero-byte snapshot, or a
  ZIP lacking `metadata.json` / `collector_status.json`.

**When:**
- The harness performs its snapshot assertions.

**Then:**
- The harness exits 1 with a message naming the missing/empty artifact.
- No "PASSED" line is printed.

---

### TC-BIS-03: Invalid — no image reference supplied

**Rule:** BIS-R001 — misuse guard

**Scenario:** The harness is invoked without an image to smoke.

**Given:**
- Neither `$1` nor `$SIGNALS_SMOKE_IMAGE` is set.

**When:**
- `bash scripts/smoke-built-image.sh` is run.

**Then:**
- The harness exits 2 with a usage message; no containers are started.

---

### TC-BIS-04: Failure — broken artifact blocks publish

**Rule:** BIS-R006, BIS-INV1 / BIS-B2 — release gate

**Scenario:** The built image's entrypoint/binary/permissions are broken
so the API never becomes healthy or a collection cannot complete.

**Given:**
- An image under test whose runtime is broken (e.g. wrong entrypoint,
  missing binary, or `/data` not writable by the non-root user).

**When:**
- The smoke runs as the release `built-image-smoke` job.

**Then:**
- The harness exits 1 (container logs tailed for diagnosis).
- The `publish` job — which `needs: built-image-smoke` — does not run, so
  the broken image is not published.

---

### TC-BIS-05: Invariant — cleanup on every exit

**Rule:** BIS-R007

**Scenario:** The harness is run to completion or fails partway.

**Given:**
- Any run (success or failure).

**When:**
- The harness exits for any reason.

**Then:**
- The PostgreSQL and Signals containers, the smoke Docker network, and the
  temp working directory it created are removed.
