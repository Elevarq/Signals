# Elevarq Signals

Elevarq Signals is a **read-only PostgreSQL diagnostic collector**. It runs on
your infrastructure, collects statistics from your databases, and packages them
as portable snapshots. No data leaves your machine. No AI. No cloud. Just
structured evidence from the views PostgreSQL already exposes.

Free and open source (BSD-3-Clause). From [Elevarq](https://elevarq.com) —
PostgreSQL tools for engineering teams.

## What it collects — and what it does not

Signals reads PostgreSQL's own **statistics, configuration, and catalog
metadata** from the `pg_stat_*` views and system catalogs — not the rows in
your application tables. Everything is collected locally and stored on disk;
there is no telemetry, no phone-home, and no external network call.

- **Read-only by design** — three independent enforcement layers prevent any
  write operation. Unsafe roles (superuser, replication) are blocked before
  collection starts.
- **No cloud, no phone-home** — all data stays on your machine.
- **No AI inside** — a pure data collector. No language models, no scoring, no
  recommendations. What you collect is what you get.
- **Built for restricted environments** — runs airgapped, as a non-root
  container, with no internet dependency.

## Why this image

- **Multi-arch** manifests: `linux/amd64` and `linux/arm64`. `docker pull`
  selects the right architecture automatically.
- **Signed with cosign** (keyless, GitHub OIDC) on every release.
- **SBOM + SLSA provenance** attached as OCI attestations.
- **Hardened runtime**: non-root (UID 10001) on a minimal Alpine base.
- Same manifest digest as the GHCR image (`ghcr.io/elevarq/signals`) — the two
  registries are mirrors of one build, not separate release processes.
- Aligned with SOC 2 / ISO 27001 engineering practices.

## Quick start

Bring your own PostgreSQL:

```bash
docker pull elevarq/signals:latest

docker run -d --name signals \
  -e SIGNALS_TARGET_HOST=host.docker.internal \
  -e SIGNALS_TARGET_USER=signals \
  -e SIGNALS_TARGET_DBNAME=postgres \
  -e SIGNALS_TARGET_PASSWORD_ENV=PG_PASSWORD \
  -e PG_PASSWORD=your_password \
  -e SIGNALS_ALLOW_INSECURE_PG_TLS=true \
  -e SIGNALS_ENV=dev \
  -v signals-data:/data \
  -p 8081:8081 \
  elevarq/signals:latest
```

For a full local trial with a pre-configured PostgreSQL, use the Docker Compose
quick start in the [README](https://github.com/Elevarq/Signals#try-it-in-2-minutes).

## Verify the image

```bash
cosign verify elevarq/signals:<version> \
  --certificate-identity-regexp='github.com/Elevarq/(Signals|signals)/.github/workflows/release.yml@' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com'
```

The certificate identity is bound to the release workflow, not the registry, so
the same command verifies `ghcr.io/elevarq/signals:<version>`. Inspect the SBOM
with `cosign download sbom elevarq/signals:<version>`. Full operator checklist:
[`docs/release-verification.md`](https://github.com/Elevarq/Signals/blob/main/docs/release-verification.md).

## Tags

| Tag | Meaning |
|---|---|
| `1.6.0` | Exact release — **pin this in production** |
| `1.6` | Latest patch of the 1.6 line |
| `1` | Latest minor of the 1.x line |
| `latest` | Newest **stable** release — convenience only |

Only CI publishes tags. `latest` moves only on a stable release; pre-releases
(`-rc`, `-beta`) never move it. For production, pin the exact version.

## Links

- Source, docs, and issues: https://github.com/Elevarq/Signals
- GHCR mirror: `ghcr.io/elevarq/signals`
- Helm chart (OCI): `oci://ghcr.io/elevarq/charts/signals`
