# Arq Signals v0.1.0 — Initial Release

The first public release of Arq Signals, an open-source PostgreSQL
diagnostic signal collector.

## What's included

- **12 versioned SQL collectors** covering server configuration,
  active sessions, database statistics, table and index I/O, query
  statistics, and transaction wraparound detection
- **Three-layer read-only enforcement** to guarantee Arq Signals
  never modifies your database
- **Portable snapshot format** (`arq-snapshot.v1`) for transferring
  collected data to downstream tools
- **Configurable scheduling** from 5-minute to 7-day cadences per
  collector, with automatic catch-up skipping
- **Multi-target support** with concurrent collection and per-target
  timeout budgets
- **Local HTTP API** and **CLI** for operations
- **Docker image** with non-root runtime on Alpine 3.20

## Why open source

We believe database diagnostic collection should be transparent. You
should be able to read every SQL query that runs against your database,
audit the binary, and own the output. Arq Signals is licensed under
BSD-3-Clause with no usage limits or feature gates.

## Intentionally out of scope

Arq Signals is a collector, not an analyzer. The following capabilities
are deliberately excluded from this project:

- Scoring, grading, or health assessments
- Recommendations or remediation guidance
- AI, LLM, or machine learning integration
- Cloud connectivity or external service calls

These capabilities belong in a downstream analyzer. Arq Signals
snapshots are compatible with [Arq Analyzer](https://elevarq.com/analyzer)
but can be consumed by any tool that reads JSON.

## Get started in 2 minutes

```bash
git clone https://github.com/elevarq/arq-signals.git
cd arq-signals
docker compose -f examples/docker-compose.yml up -d
```

Wait for PostgreSQL to become healthy, then trigger a collection:

```bash
curl -X POST http://localhost:8081/collect/now \
  -H "Authorization: Bearer test-token"
```

Export the collected data:

```bash
curl -o snapshot.zip http://localhost:8081/export \
  -H "Authorization: Bearer test-token"
```

Inspect the snapshot:

```bash
unzip -l snapshot.zip
```

## What's next

- Additional collectors (replication, locks, WAL statistics)
- Kubernetes deployment examples
- Expanded documentation
- Community-contributed collectors
