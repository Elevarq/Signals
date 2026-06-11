-- TimescaleDB demo fixture — roles + demo database (issue #76).
-- Runs once at first container start (docker-entrypoint-initdb.d),
-- executed as the postgres superuser BEFORE seed.sql.
--
-- Role model (docs/postgres-role.md § TimescaleDB targets):
--
--   tsdemo_owner       Owns the demo database and every fixture
--                      object/job, so the job_errors visibility demo
--                      does not require granting membership in a
--                      superuser role. Must be LOGIN: TimescaleDB
--                      runs policy/compression background workers AS
--                      the owning role and refuses to start them for
--                      a NOLOGIN role ("permission denied to start
--                      background process"). The password exists only
--                      to satisfy LOGIN; nothing connects with it.
--   arq_monitor        Least-privilege collector role: LOGIN +
--                      pg_monitor. Sees the full hypertable / chunk /
--                      compression / jobs / job_stats surface, and
--                      ZERO rows in timescaledb_information.job_errors
--                      — the documented partial-by-design state.
--   arq_monitor_owner  Same, plus membership in tsdemo_owner (the
--                      database-owner role), which is what unlocks
--                      job_errors row visibility.

CREATE ROLE tsdemo_owner LOGIN PASSWORD 'tsdemo_owner_pass'
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;

CREATE ROLE arq_monitor LOGIN PASSWORD 'monitor_pass'
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
GRANT pg_monitor TO arq_monitor;

CREATE ROLE arq_monitor_owner LOGIN PASSWORD 'monitor_owner_pass'
    NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;
GRANT pg_monitor TO arq_monitor_owner;
GRANT tsdemo_owner TO arq_monitor_owner;

-- The demo database. The timescale image installs the extension into
-- template1, so tsdemo inherits it; the explicit CREATE EXTENSION in
-- seed.sql is a belt-and-suspenders no-op in that case.
CREATE DATABASE tsdemo OWNER tsdemo_owner;
