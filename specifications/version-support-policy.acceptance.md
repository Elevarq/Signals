# Acceptance Tests: version-support-policy

## Feature

`specifications/version-support-policy.md` — the cross-engine version support
tiers (#625), the MySQL 8.0 + 8.4 + 9.7 live qualification, and the MariaDB
10.11 + 11.4 + 11.8 + 12.3 LTS live qualification (#638).

## Test Cases

### TC-VSP-01: MySQL 8.0 AND 8.4 are both live-qualified in CI (structural)

**Rule:** VSP-R001, VSP-R002, INV-VSP-01

**Given:** `.github/workflows/ci.yml`. **When:** the `integration-mysql` job's
matrix is parsed. **Then:** it has ≥3 legs covering `8.0`, `8.4`, and `9.7`, each
a pinned image digest. **Expected Result:** Pass —
`TestMySQLVersionMatrixQualifies80And84`. Guards against silently dropping any
qualified leg (which would leave a support claim without evidence).

---

### TC-VSP-02: each qualified range is a live qualification with all collectors success (live)

**Rule:** VSP-R001, VSP-R003

**Given:** the pinned per-engine CI images. **When:** the `integration-*` live
qualifications run. **Then:** every collector persists `status=success` through
the real daemon → exporter for each qualified image (PostgreSQL 14–18, MySQL
8.0 + 8.4 + 9.7, MariaDB 10.11 + 11.4 + 11.8 + 12.3, SingleStore 9.1.x,
ClickHouse 24.8). **Expected Result:** Confirmed by the `integration-*` jobs (each
engine's `*LiveCadenceQualification`).

---

### TC-VSP-04: the four MariaDB LTS lines are each live-qualified in CI (structural)

**Rule:** VSP-R001, VSP-R006, INV-VSP-01

**Given:** `.github/workflows/ci.yml`. **When:** the `integration-mariadb` job's
matrix is parsed. **Then:** it has four legs covering `10.11`, `11.4`, `11.8`, and
`12.3`, each a pinned image digest. **Expected Result:** Pass —
`TestMariaDBVersionMatrixQualifiesLTS`. Guards against silently dropping any
qualified MariaDB LTS leg (which would leave a support claim without evidence).
MariaDB qualification is independent of MySQL's (VSP-R006).

---

### TC-VSP-03: unknown/best-effort versions are collected, not rejected (boundary)

**Rule:** VSP-R004, INV-VSP-02

**Given:** a version outside a qualified range. **When:** collected. **Then:** it
is collected best-effort (no runtime version rejection), `engine_version` is
recorded truthfully, and any collector whose view is absent on that version
degrades to a legitimate empty/skipped result — never a fabricated success.
**Expected Result:** Documented; the degrade path is each engine's permission/
availability handling (e.g. CHB-R004, MYC-R006/R007). No code claims
qualification without CI evidence.
