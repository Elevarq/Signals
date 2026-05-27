# Launch Announcement

## Long Version

**Arq Signals: Open-Source PostgreSQL Diagnostic Signal Collector**

We are releasing Arq Signals, an open-source tool that collects diagnostic signals from PostgreSQL databases. It runs 12 versioned SQL collectors against your instances, stores the results locally in SQLite, and exports self-contained snapshots you can archive, analyze, or feed into any downstream tooling.

**Why open-source a collector?**

Diagnostic data collection is infrastructure plumbing. It should be transparent, auditable, and under your control. Every SQL query Arq Signals runs is visible in the source code, statically linted at startup to guarantee read-only behavior, and enforced at three independent layers: a static linter that rejects DDL/DML before the process starts, session-level `default_transaction_read_only`, and per-query `BEGIN READ ONLY` transactions.

**What it collects.**

Statistics from PostgreSQL's built-in catalog views: `pg_stat_activity`, `pg_stat_database`, `pg_stat_user_tables`, `pg_settings`, `pg_stat_statements` (when available), and transaction ID wraparound metrics. Collectors are filtered automatically based on your PostgreSQL version and installed extensions. Nothing runs that your instance does not support.

**The security model is simple.**

Arq Signals makes no outbound network connections except to your PostgreSQL targets. There is no telemetry, no analytics, no cloud sync. Credentials are used at connection time and never persisted to storage or included in exports. The API binds to loopback by default.

**Get started in two minutes.**

```
git clone https://github.com/elevarq/arq-signals.git
cd arq-signals && make build
./arqctl collect --dsn postgres://monitor@localhost:5432/mydb
./arqctl export --output snapshot.zip
```

Docker images are available at `ghcr.io/elevarq/arq-signals`.

**What is next.**

We will be adding more collectors, improving snapshot tooling, and documenting integration patterns. Contributions are welcome -- see CONTRIBUTING.md for guidelines.

GitHub: [github.com/elevarq/arq-signals](https://github.com/elevarq/arq-signals)
License: BSD-3-Clause

---

## Short Version

**Arq Signals** -- open-source PostgreSQL diagnostic signal collector.

Runs read-only SQL collectors against your Postgres instances, stores results locally, exports versioned snapshots. No AI, no cloud, no telemetry. Every query is visible in the source, statically linted, and enforced read-only at three layers.

12 collectors. Cadence scheduling. Docker-ready. BSD-3-Clause.

`git clone https://github.com/elevarq/arq-signals.git && make build && ./arqctl collect`

GitHub: github.com/elevarq/arq-signals
