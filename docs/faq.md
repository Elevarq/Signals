# Frequently Asked Questions

## Does Elevarq Signals send my data anywhere?

No. All data stays on your machine. Elevarq Signals makes no outbound connections except to your PostgreSQL databases. There is no telemetry, analytics, or phone-home functionality.

## Does Elevarq Signals use AI?

No. Elevarq Signals is a data collector. It does not contain any AI, machine learning, or language model functionality. It runs SQL queries and stores the results.

## Does Elevarq Signals require internet access?

Only to reach your PostgreSQL targets. If your databases are on the same network, Elevarq Signals works fully offline. It does not download updates, sync to a cloud, or contact external services.

## What data does Elevarq Signals collect?

Statistics from PostgreSQL's built-in views: `pg_stat_activity`, `pg_stat_database`, `pg_stat_user_tables`, `pg_settings`, `pg_stat_statements` (if available), and wraparound detection queries. All queries are read-only and visible in the source code.

## Can I review the SQL queries it runs?

Yes. Every query is defined in `internal/pgqueries/catalog.go` and `catalog_wraparound.go`. They are statically linted at startup to ensure they contain only SELECT statements with no DDL, DML, or dangerous functions.

## How does Elevarq Signals relate to Elevarq Analyzer?

Elevarq Signals is the open-source data collector. Elevarq Analyzer is a separate, commercial product that can ingest Elevarq Signals snapshots for automated analysis and reporting. Elevarq Signals is fully functional on its own -- you do not need Elevarq Analyzer to use it.

## Can I use Elevarq Signals without buying anything?

Yes. Elevarq Signals is free and open source under the BSD-3-Clause license. There are no paid tiers, usage limits, or feature gates.

## Can Elevarq Signals modify my database?

No. Connections enforce read-only mode at three independent layers. The SQL linter will abort the process at startup if any query contains write operations.

## What PostgreSQL versions are supported?

PostgreSQL 14 and later (actively supported versions). Some collectors require specific extensions (e.g., `pg_stat_statements`) which are automatically detected and skipped if unavailable.

## How do I integrate Elevarq Signals with my existing monitoring?

Elevarq Signals exports snapshots as ZIP archives with a documented JSON format (`signals-snapshot.v1`). You can feed these into any downstream tool, script, or pipeline. The format is stable and versioned.

## What happens if I connect with a superuser role?

Elevarq Signals will refuse to collect. The system validates the connected
role's attributes before each collection cycle and blocks collection if
the role has superuser, replication, or bypass RLS privileges. Create a
dedicated monitoring role with `pg_monitor` instead. An explicit
override (`SIGNALS_ALLOW_UNSAFE_ROLE=true`) exists for lab/dev use
but is not recommended for production.

## Does pg_stat_statements work across PostgreSQL versions?

Yes. The `pg_stat_statements` collector uses dynamic column capture
(a wildcard projection over the view) so it adapts to whatever
columns the installed extension version exposes. This avoids
failures caused by columns renamed or added between PostgreSQL
releases (e.g. `blk_read_time` was renamed to
`shared_blk_read_time` in PostgreSQL 17). If the extension is not
installed, the collector is silently skipped.

## Does the pg_stat_statements collector return Signals' own queries?

No. The collector self-filters so that customer workload analysis
is not polluted by Signals' own probe queries:

- Every PostgreSQL connection opened by Signals sets
  `application_name = signals` in its startup parameters.
- The `pg_stat_statements_v1` SQL excludes rows attributable to
  any session whose `application_name` is `signals`,
  matched on `(userid, dbid)`.
- The same SQL scopes rows to the connected database
  (`pg_database.datname = current_database()`), so cross-database
  statistics from other databases on the same cluster are not
  collected.

If you operate another application that also sets its
`application_name` to `signals`, its rows will be suppressed
too. Pick a distinct name for non-Signals tooling.
