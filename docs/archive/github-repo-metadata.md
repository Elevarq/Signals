# GitHub Repository Metadata

Recommended settings for the Arq Signals GitHub repository to maximize
discoverability and trust.

## Repository settings

### Description (About)

```
Read-only PostgreSQL diagnostic collector. No AI, no cloud, no write operations. Collects statistics from pg_stat views and packages them as portable snapshots. BSD-3-Clause.
```

### Homepage

```
https://elevarq.com/signals
```

(If no dedicated page exists yet, leave blank or use the repo URL.)

### Topics

```
postgresql
postgres
monitoring
diagnostics
database
dba
observability
read-only
data-collection
pg-stat-statements
airgapped
open-source
golang
```

**Rationale for topic selection:**

| Topic | Why |
|-------|-----|
| postgresql, postgres | Primary search terms for PostgreSQL tools |
| monitoring | Core function — collecting diagnostic signals |
| diagnostics | Distinguishes from metrics/alerting tools |
| database | Broad category for database tooling searches |
| dba | Target audience |
| observability | Industry category |
| read-only | Key differentiator and trust signal |
| data-collection | Describes the function without implying analysis |
| pg-stat-statements | Specific PostgreSQL feature — attracts people searching for it |
| airgapped | Attracts users in restricted environments |
| open-source | Helps filter searches |
| golang | Language community discovery |

### Social preview

If creating a social preview image, the message should be:

```
Arq Signals
Read-only PostgreSQL diagnostic collector
No AI · No cloud · Every query visible in source
BSD-3-Clause
```

## Issue labels

Create these labels for community engagement:

| Label | Color | Description |
|-------|-------|-------------|
| `good first issue` | #7057ff | Good for newcomers |
| `help wanted` | #008672 | Extra attention is needed |
| `new-collector` | #0e8a16 | Request for a new SQL collector |
| `documentation` | #0075ca | Improvements or additions to documentation |
| `bug` | #d73a4a | Something isn't working |
| `enhancement` | #a2eeef | New feature or request |
| `security` | #ee0701 | Security-related issue |
| `question` | #d876e3 | Further information is requested |
| `wontfix` | #ffffff | Out of scope (analysis, scoring, AI) |

### Labeling strategy

- **`good first issue`**: Apply to well-scoped documentation fixes,
  config validation improvements, or adding new test cases. These help
  new contributors get started.

- **`help wanted`**: Apply to collector contributions (new SQL queries
  for pg_stat views), Kubernetes examples, and platform-specific
  deployment guides.

- **`new-collector`**: Apply to requests for additional PostgreSQL
  diagnostic queries. These are the highest-value community
  contributions and should be clearly welcoming.

- **`wontfix`**: Apply to requests for analysis, scoring, AI, or
  recommendation features. Link to the "What Arq Signals does NOT do"
  section and redirect to Arq Analyzer if appropriate.

## Pinned sections in README

The README is structured so the first screen shows:

1. **Title + value proposition** (one paragraph)
2. **Trust banner** (read-only, no cloud, no AI, restricted environments)
3. **2-minute quickstart** (clone → compose up → curl → inspect)

This ordering is deliberate. A DBA or SRE landing on the repo should
understand what it does, trust that it's safe, and be able to try it —
all within the first scroll.

## Repository features to enable

- **Discussions**: Enable for community Q&A (reduces noise in Issues)
- **Wiki**: Disable (documentation lives in docs/)
- **Projects**: Optional (useful for tracking collector contributions)
- **Sponsorship**: Disable (commercial support via Arq Analyzer)
