# scripts/

Developer-facing helpers. Operator install / runtime docs live
under [`docs/`](../docs/).

## `preflight.sh`

Runs the same Go gates `.github/workflows/ci.yml` runs, locally.
The script is the single source of truth — CI calls it per gate;
the pre-push hook calls it before every push so failing-gofmt
PRs never reach GitHub.

```sh
scripts/preflight.sh             # default: run `all` (gofmt + vet + build + test + security)
scripts/preflight.sh gofmt       # ~1 second
scripts/preflight.sh vet         # ~2 seconds
scripts/preflight.sh build       # ~3 seconds
scripts/preflight.sh test        # full Go suite with -race; ~30 seconds
scripts/preflight.sh secrets     # gitleaks (staged + current commit); ~3 seconds
scripts/preflight.sh vuln        # govulncheck ./...; ~5 seconds
scripts/preflight.sh semgrep     # SAST via p/golang + p/security-audit; ~30 seconds
scripts/preflight.sh osv         # osv-scanner --recursive .; ~2 seconds
scripts/preflight.sh kube-lint   # kube-linter + conftest against rendered Helm chart
scripts/preflight.sh lint        # golangci-lint run ./...
scripts/preflight.sh security    # secrets + vuln + semgrep + osv + kube-lint + lint, fail-fast
```

The `security` bundle (#160) is local-fast — full-history Gitleaks,
SBOM, Grype, and OpenSSF Scorecard live in scheduled/release CI so
push-time stays cheap. Each gate emits an actionable `Install
locally with: …` hint when its tool is missing, and exits 127 so
CI / pre-push hooks can distinguish a missing-tool skip from a
real finding.

## Pre-push hook (recommended)

After cloning the repo, wire `.githooks/pre-push` into your local
`.git/hooks/` so `scripts/preflight.sh all` runs automatically
before every push:

```sh
ln -s ../../.githooks/pre-push .git/hooks/pre-push
chmod +x .git/hooks/pre-push
```

(One-time setup per clone — git tracks `.githooks/` but not
`.git/hooks/`.)

Bypass for an emergency push:

```sh
git push --no-verify
```

## Why this exists

Tracked under [Elevarq/Arq-Signals#141](https://github.com/Elevarq/Arq-Signals/issues/141).
Sibling of the same pattern shipped on Workbench
([Elevarq/Arq-Workbench#255](https://github.com/Elevarq/Arq-Workbench/issues/255) /
[Elevarq/Arq-Workbench#261](https://github.com/Elevarq/Arq-Workbench/pull/261)) —
unified developer ergonomics across the three Go repos.

Catching gofmt / vet / build / test failures pre-push removes the
push → watch-CI-fail → fix → push-again round-trip cost. CI and
the local hook call the SAME script, so there's no drift between
what runs locally and what runs on GitHub.
