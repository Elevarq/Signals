# Security Policy

## Supported versions

| Version | Supported |
|---------|-----------|
| Latest  | Yes       |

## Local development gates

Arq Signals enforces its security posture with the same local-first
gate pattern as the rest of the Elevarq train. The Go gates + the
new security gates share one entrypoint:

```sh
scripts/preflight.sh             # gofmt + vet + build + test + security
scripts/preflight.sh secrets     # gitleaks staged + current-commit
scripts/preflight.sh vuln        # govulncheck ./...
scripts/preflight.sh lint        # golangci-lint run ./...
scripts/preflight.sh security    # secrets + vuln + lint
```

Each gate emits an actionable install hint when its tool is
missing and exits 127 (distinct from a real finding's exit 1), so
CI / pre-push hooks can distinguish a missing-tool skip from a
genuine secret / CVE / lint failure.

The pre-push hook wired via `.githooks/pre-push` calls
`scripts/preflight.sh all`, so failing-gate PRs never reach
GitHub.

## CI evidence (in progress)

Tracked under issue [#160](https://github.com/Elevarq/Arq-Signals/issues/160):

- **#161** ✓ — push/PR CI gate runs `preflight.sh secrets` +
  `preflight.sh vuln`. (`lint` joined the gate in #173 once the
  default-linter baseline was cleaned.)
- **#173** ✓ — `.golangci.yml` pinned, errcheck idiomatic-Close /
  Rollback / Encode carve-outs documented in-config, 50-finding
  baseline cleaned, `preflight.sh lint` promoted to a push/PR-
  required check.
- **#162** ✓ — nightly evidence in
  `.github/workflows/nightly-security.yml`: full-history Gitleaks,
  Syft SBOM (SPDX-JSON), Grype against the SBOM, OpenSSF Scorecard.
  Artefacts upload to the workflow run + Security tab (SARIF).
- **#163** ✓ — Semgrep (`p/golang` + `p/security-audit`) and
  OSV-Scanner run as `preflight.sh semgrep` / `preflight.sh osv`,
  folded into the `security` bundle. CI runs both on every push/PR
  alongside the secrets + vuln gates.
- **#164** ✓ — `preflight.sh kube-lint` runs `kube-linter` +
  `conftest` against the rendered Helm chart under
  `deploy/helm/arq-signals`. Conftest policies live in
  `policy/security.rego` (defence-in-depth:
  `automountServiceAccountToken=false`, no
  hostNetwork/PID/IPC, runAsNonRoot, drop all caps, no
  privilege escalation).
- **#165** ✓ — release workflow signs the published container
  image with cosign (keyless via GitHub Actions OIDC + Sigstore
  Fulcio) for both GHCR and Docker Hub. SBOM is re-attested via
  `cosign attest --type spdxjson` so `cosign verify-attestation`
  succeeds against the workflow OIDC identity. Verification
  commands live in `docs/release-verification.md`.

## Reporting a vulnerability

If you discover a security vulnerability in Arq Signals, please report it
responsibly:

1. **Do not** open a public GitHub issue
2. Email security@elevarq.com with:
   - Description of the vulnerability
   - Steps to reproduce
   - Potential impact
3. We will acknowledge receipt within 48 hours
4. We will provide a fix timeline within 5 business days

## Security model

### PostgreSQL connections

Arq Signals enforces read-only access through three independent layers:

1. **Static linting**: All SQL queries are validated at startup. DDL, DML, and
   dangerous functions cause the process to abort immediately.
2. **Session-level**: Connections use `default_transaction_read_only=on`.
3. **Per-query**: Each query runs inside `BEGIN ... READ ONLY`.

### Credentials

- Passwords are read from file or environment variable at connection time
- Passwords are never cached in memory beyond a single connection attempt
- Passwords are never written to SQLite
- Passwords are never included in snapshot exports
- Password rotation is supported (re-read on each connection)

### Network

- The HTTP API binds to a configurable address (default `127.0.0.1:8081`)
- Arq Signals makes no outbound network connections except to PostgreSQL targets
- No data is sent to external services, AI providers, or analytics platforms

### Data handling

- Snapshots contain only PostgreSQL statistics view data
- No credentials, DSNs, or secrets appear in exports
- SQLite database is stored locally with no remote replication

### Container hardening (when deployed via Docker)

- Non-root runtime (UID 10001)
- Minimal Alpine base image
- No shell or compilers in production image
