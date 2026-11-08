# CI Supply-Chain Tool Pinning

## Status

ACTIVE

## Issue

Elevarq/Signals#733 (audit A8 from the code review; supply-chain hardening).

## Purpose

The CI Helm/Kubernetes gate installs two third-party tools — `kube-linter`
and `conftest` — by downloading versioned release tarballs over HTTPS and
piping them straight into `tar -x` under `/usr/local/bin`. A fixed version
string alone does not pin *content*: mutable or replaced upstream bytes at the
same tag would be extracted and executed with no detection. This specification
requires that CI verify each tool's tarball against a pinned SHA-256 before
extraction and fail closed on any mismatch, so unverified bytes are never run.

This is hardening against a supply-chain threat, not a response to an observed
compromise.

## Scope

In scope:

- `scripts/install-pinned-tool.sh` — stage a download, verify its SHA-256
  against a pinned expected digest, and extract a single member only on match.
- The `Install kube-linter` and `Install conftest` steps in
  `.github/workflows/ci.yml`, rewired to call that script with pinned digests.
- A test proving the verifier accepts a matching tarball and rejects an altered
  one before extraction/execution.

Out of scope (separate, pre-existing policy work):

- Pinning GitHub Action / Docker references (#28) and hashing the Semgrep
  installer (#45) — these cover different installers.
- Dynamic Semgrep rules, Alpine package reproducibility, gitleaks exemptions.
- Cosign/sigstore attestation verification of these tools (the pinned-digest
  approach is the chosen equivalently-verified mechanism; an attestation path
  may be added later without weakening this one).

## Interfaces

- Inputs to `install-pinned-tool.sh`: `<url> <sha256> <member> <dest-dir>`.
  `url` is `https://` in CI and may be `file://` in tests; `sha256` is the
  expected lowercase 64-hex-char digest of the downloaded tarball; `member` is
  the single archive entry to extract; `dest-dir` is an existing directory.
- Outputs: process exit status (0 = verified + extracted; non-zero = fail
  closed) plus a human-readable line on success and a diagnostic on failure.
- Pinned digests (recorded in `ci.yml`, updated via the procedure below):
  - `kube-linter-linux.tar.gz` @ v0.8.3 —
    `1a6d8419b11971372971fdbc22682b684ebfb7cf1c39591662d1b6ca736c41df`
  - `conftest_0.62.0_Linux_x86_64.tar.gz` @ 0.62.0 —
    `284231908e4cf66d156e5577c3eee2f67d9f2edf2521fa2309e0df1969f871e5`
    (matches upstream `checksums.txt`).

## Behaviors

### B1 — Matching digest extracts and succeeds

Given a tarball whose SHA-256 equals the pinned expected digest, When
`install-pinned-tool.sh` runs, Then it extracts the requested member into the
destination and exits 0.

### B2 — Mismatched digest fails closed before extraction

Given a tarball whose bytes differ from the pinned digest (tampered or
replaced), When the installer runs, Then it exits non-zero, does not extract
the member, and does not place any executable in the destination.

### B3 — Missing / unavailable download fails closed

Given a URL that cannot be fetched (404/5xx/transport error after retries) or
an empty download, When the installer runs, Then it exits non-zero and extracts
nothing. Transient-network retry is preserved (`curl --retry`).

### B4 — Malformed pin fails closed

Given an expected-digest argument that is not a 64-character hex string, When
the installer runs, Then it exits non-zero before downloading or extracting.

### B5 — Verified tarball missing the member fails closed

Given a tarball that passes digest verification but does not contain the
requested member, When the installer runs, Then it exits non-zero rather than
leaving a partial/empty install.

### B6 — CI installs both tools through the verifier

Given `.github/workflows/ci.yml`, When the Helm/Kubernetes gate installs
`kube-linter` and `conftest`, Then both go through `install-pinned-tool.sh`
with a pinned digest, and neither is installed by piping a download straight
into `tar`.

## Rules

- **SCT-R001**: CI MUST verify each pinned tool tarball (`kube-linter`,
  `conftest`) against an expected SHA-256 before extraction, and MUST NOT pipe
  an unverified download directly into `tar`.
- **SCT-R002**: On digest mismatch, missing/empty download, malformed pin, or a
  verified tarball lacking the requested member, the installer MUST fail closed
  (non-zero exit) and MUST NOT extract or execute any bytes.
- **SCT-R003**: The installer MUST preserve deliberate transient-network retry
  behaviour while still failing on HTTP errors, and MUST be deterministic and
  dependency-free beyond bash, curl, tar, and a SHA-256 tool (`sha256sum` or
  `shasum -a 256`).
- **SCT-R004**: The expected digests MUST be recorded alongside their
  version/URL in `ci.yml`, and the version+digest update procedure MUST be
  documented so a bump is a reviewable, auditable change.

## Invariants

- **INV-SCT-01**: CI never extracts or executes tool bytes whose content
  identity has not matched a pinned digest.
- **INV-SCT-02**: A tool version bump cannot land without an accompanying
  digest update — a changed URL with a stale digest fails the gate.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Downloaded tarball SHA-256 ≠ pinned digest | installer exits 1, nothing extracted (SCT-R002). |
| Download 404/5xx/transport error or empty body | installer exits 1 after retries, nothing extracted (SCT-R002/R003). |
| Expected-digest argument not 64 hex chars | installer exits 1 before any download (SCT-R002). |
| Verified tarball missing the requested member | installer exits 1, no partial install (SCT-R002). |

## Constraints

- Dependency-free beyond bash, curl, tar, awk, grep, and a sha256 tool.
- Deterministic and CI-stable; no AWS, no PostgreSQL, no network in the test
  (fixtures use `file://`).

## NFR overrides

None. This strengthens the supply-chain posture (verifiable tool provenance in
CI); it weakens no existing guarantee.

## Version + digest update procedure

To bump a pinned tool:

1. Change the version in the `ci.yml` install step.
2. Recompute the digest from the released asset
   (`curl -sSfL <url> | sha256sum`), cross-checking the upstream
   `checksums.txt` / signed attestation where published.
3. Update the pinned digest next to the version in `ci.yml` and the
   Interfaces list above, in the same reviewed PR.

## Rollback / recovery

The script and the `ci.yml` wiring are additive CI hardening. Reverting the
wiring restores the prior (unverified) install with no runtime impact on the
shipped product; the pinned digests are inert data.
