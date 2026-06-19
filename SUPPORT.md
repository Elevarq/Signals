# Support

How to get help with Elevarq Signals, what to expect, and which versions are
supported.

> **Security vulnerabilities do not go here.** Report them privately per
> [SECURITY.md](SECURITY.md) — never in a public issue.

## Getting help

| You want to… | Use |
|---|---|
| Report a bug / regression | [Open a GitHub issue](https://github.com/Elevarq/signals/issues/new) with the details below |
| Request a feature or collector | GitHub issue, labelled as an enhancement |
| Ask a usage / configuration question | Start with the [docs](docs/) — [Architecture](docs/architecture.md), [Database connections](docs/database-connections.md), [Production install](docs/install/kubernetes-production.md), [FAQ](docs/faq.md) — then open a GitHub issue if unanswered |
| Report a security vulnerability | **[SECURITY.md](SECURITY.md)** (private advisory / `security@elevarq.com`) |
| Commercial / enterprise support | Contact Elevarq via [elevarq.com](https://elevarq.com) |

### What to include in a bug report

A complete report is the fastest path to a fix:

- Signals image tag and Helm chart version (`signalsctl version`).
- PostgreSQL major version and hosting (RDS / Cloud SQL / self-managed / …).
- The `auth_method` in use and `sslmode`.
- What you expected vs. what happened, with the relevant **audit log** lines
  and the output of `signalsctl doctor` and `GET /status`.
- A **support bundle** if possible — see below.

Audit logs, `doctor`, and `/status` are designed to be safe to share: they
emit **metadata only** and never credentials, connection strings, or row
payloads. Still, review before posting publicly.

### Support bundle

For anything non-trivial, gather a support bundle so a maintainer can diagnose
without back-and-forth:

- `signalsctl doctor` output (pre-flight checks: connectivity, role safety,
  collector prerequisites, snapshot freshness).
- `GET /status` (per-target state, circuit-breaker state, freshness counters).
- The daemon's recent structured logs.
- The relevant collector config (with secrets removed — they are never in the
  config anyway).

See [observability/operational-readiness.md](docs/observability/operational-readiness.md)
for the health/status surfaces and the closed failure taxonomy.

## Response targets

Elevarq Signals is maintained by a small team. We triage on a **best-effort**
basis and do not offer a paid SLA on the open-source project. Typical targets:

| Severity | Definition | Best-effort first response |
|---|---|---|
| **S1 — Critical** | Data-collection unusable for all users, or a safety guarantee (read-only enforcement, credential non-persistence) is violated | 2 business days |
| **S2 — High** | A core path broken with no reasonable workaround | 5 business days |
| **S3 — Medium** | Broken behaviour with a workaround, or a significant doc gap | best-effort |
| **S4 — Low** | Minor issue, cosmetic, or enhancement request | best-effort |

These are **targets, not guarantees**, for the open-source project. Security
issues follow the separate, faster disclosure timeline in
[SECURITY.md](SECURITY.md) (acknowledge within two business days; fix or
documented mitigation within ten business days of acknowledgement for
High/Critical). Formal, time-bound SLAs are available under a commercial
support agreement — contact Elevarq via [elevarq.com](https://elevarq.com).

## Supported versions

Version support and end-of-life follow the single table in
[SECURITY.md → Supported versions](SECURITY.md#supported-versions): the most
recent tagged release on the active line receives fixes; earlier lines are
end-of-life. We do not back-port fixes to end-of-life versions — upgrade to the
current release.
