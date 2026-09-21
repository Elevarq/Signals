# Built-Image Smoke — Post-Deploy Verification of the Container Artifact

## Status

ACTIVE

## Purpose

Close the "green CI, broken artifact" gap for Signals. Source-level
`go test` (including the `integration` build tag against ephemeral
PostgreSQL) exercises the compiled *source*, never the *shipped image*.
A broken `Dockerfile` entrypoint, a missing baked binary, a `/data`
volume permission wrong for the non-root user, or a runtime-config wiring
mistake can therefore ship with a fully green pipeline — exactly the
false-confidence class the org-wide overhaul (Elevarq/elevarq-website#529)
exists to kill, and the smoke mandated by Release Protocol Gate A step 4
("smoke the built artifact, not only source").

This specification defines a **self-contained smoke that runs the actual
built container image against a real PostgreSQL and asserts it completes a
real collect → export cycle producing a well-formed snapshot.** It runs on
every PR (fast, single-arch) and as a required pre-publish gate in the
release pipeline, so a broken artifact blocks publish.

## Scope

- **In scope:** verifying the built image can connect to a real
  PostgreSQL, run one collection cycle, and export a non-empty,
  well-formed snapshot ZIP; wiring that check into PR CI and as a gate
  the release `publish` job depends on.
- **Out of scope:** cloud/passwordless onboarding paths (covered by the
  operator-gated `aws-rds-iam-live-smoke.yml`), multi-target behavior,
  collector output-contract correctness (covered by the `integration`
  tests), and any diagnosis/scoring (Signals emits evidence only).

This is a **behavioral** specification: the smoke exercises real code
behavior in the real artifact, so tests are mandatory (STDD v2).

## Interfaces

### Smoke harness — `scripts/smoke-built-image.sh`

| Input | Type | Constraints |
|-------|------|-------------|
| image reference | `$1` or `$SIGNALS_SMOKE_IMAGE` | required; a locally available Docker image tag/digest for the artifact under test |

The harness builds nothing — the caller is responsible for building/loading
the image so the smoke exercises exactly the artifact under test.

| Output | Meaning |
|--------|---------|
| exit 0 | the built image collected against real PostgreSQL and produced a well-formed snapshot |
| exit 1 | a smoke assertion failed (unhealthy API, failed collection, missing/empty/malformed snapshot) |
| exit 2 | misuse (no image reference, or the representative seed is absent) |

### Fixture

The ephemeral PostgreSQL is seeded from the repo's own representative
seed `examples/init.sql` (non-superuser `signals` monitoring role +
representative schema and data), with `pg_stat_statements` preloaded. The
smoke reuses the shipped example fixture rather than a bespoke one, so it
tracks the product's documented deployment shape.

## Behaviors

- **BIS-B1 — Happy path.** Given the built image and a freshly seeded
  PostgreSQL, when the smoke runs, then the collector API reports healthy,
  `signalsctl collect now --force` succeeds, `signalsctl export` writes a
  snapshot, and the harness exits 0.
- **BIS-B2 — Broken artifact.** Given an image whose entrypoint/binary/
  permissions are broken such that the API never becomes healthy or a
  collection cannot complete, when the smoke runs, then the harness exits
  non-zero and the release `publish` job does not run.
- **BIS-B3 — Empty/malformed snapshot.** Given the cycle runs but no
  well-formed snapshot is produced, when the smoke asserts snapshot
  contents, then it exits 1.

## Rules

| ID | Rule |
|----|------|
| BIS-R001 | The smoke MUST exercise the built container image (its entrypoint, baked `signals`/`signalsctl` binaries, non-root user, and `/data` volume), never a source-built binary. |
| BIS-R002 | The dependency MUST be a real PostgreSQL instance; no mocks or stubs stand in for the target database. |
| BIS-R003 | The smoke MUST drive the real collect → export code path (`signalsctl collect now --force`, then `signalsctl export`), not a synthetic subset. |
| BIS-R004 | The smoke MUST assert the exported snapshot ZIP is non-empty and, where `unzip` is available, contains `metadata.json` and a `collector_status.json` payload (guards against a false-clean export, R006/R125). |
| BIS-R005 | The smoke MUST run on every pull request (single-arch, fast) so a regression fails on the introducing PR, not only at release. |
| BIS-R006 | The release `publish` job MUST depend on the smoke; a failing smoke MUST block publish of the image. |
| BIS-R007 | The smoke MUST be self-contained (no cloud/AWS credentials or external infrastructure) and MUST tear down every container, network, and temp file it creates on exit, success or failure. |

## Invariants

- **BIS-INV1** — A published release image has passed a live collect →
  export cycle against a real PostgreSQL. There is no release path that
  publishes the image without the smoke having passed.
- **BIS-INV2** — The smoke asserts against the *same* artifact that is
  published (the image built from the release commit), not a re-derived or
  source-only build.

## Failure Conditions

| Trigger | Response |
|---------|----------|
| No image reference provided, or `examples/init.sql` missing | exit 2 (misuse) |
| PostgreSQL never becomes ready / seed role never appears | exit 1 |
| Container exits before `/health` is ready | exit 1, with container logs tailed |
| `collect` or `export` command fails | exit 1 (propagated), with container logs tailed |
| Snapshot missing, empty, or malformed | exit 1 |

## Constraints

- Runs on GitHub-hosted `ubuntu-24.04` runners with Docker available.
- PR runs build a single architecture (amd64) for speed; the release
  gate smokes the amd64 build of the release commit before the multi-arch
  publish.
- Fixture PostgreSQL major tracks the shipped example (`postgres:16-alpine`);
  broad cross-major coverage remains the job of the `integration-pg` matrix.

## Traceability

`specification (this file) → acceptance cases
(built-image-smoke.acceptance.md) → harness + CI wiring
(scripts/smoke-built-image.sh, .github/workflows/ci.yml,
.github/workflows/release.yml) → features/signals/traceability.md`

Umbrella: Elevarq/elevarq-website#529. Child: Elevarq/Signals#421.
