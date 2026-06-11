-- TimescaleDB demo fixture — sample workload (issue #76).
-- Runs once at first container start, after roles.sql, as the
-- postgres superuser. Every fixture object and job is created as
-- tsdemo_owner so the job_errors ownership demo works (see roles.sql).
--
-- Determinism: all series are anchored to date_trunc('day', now()),
-- so chunk counts, compressed/uncompressed split, and cagg bucket
-- counts are stable for any demo run on a given day.
--
-- What each element produces (full map in README.md):
--   metrics            46 daily chunks (32 compressed / 14 not),
--                      space dimension, compression + retention
--                      policies, daily continuous aggregate
--   events             integer-dimension hypertable (10 chunks)
--   legacy_measurements plain time-keyed table — the future
--                      "hypertable candidate" (TS-R008) exhibit
--   demo_failing_job   fails every run → job_stats.total_failures
--                      grows; rows visible in job_errors only with
--                      db-owner membership

\c tsdemo

-- No-op on the timescale image (extension inherited via template1);
-- kept for portability to other base images.
CREATE EXTENSION IF NOT EXISTS timescaledb;

SET ROLE tsdemo_owner;

-- ---------------------------------------------------------------
-- metrics: timestamptz dimension + space dimension, 45 days of
-- hourly samples for 4 devices (~4.3k rows, 46 daily chunks).
-- ---------------------------------------------------------------
CREATE TABLE metrics (
    time      timestamptz       NOT NULL,
    device_id int               NOT NULL,
    cpu       double precision  NOT NULL,
    mem       double precision  NOT NULL
);
SELECT create_hypertable('metrics', 'time',
                         chunk_time_interval => interval '1 day');
SELECT add_dimension('metrics', 'device_id', number_partitions => 4);

INSERT INTO metrics (time, device_id, cpu, mem)
SELECT g, d,
       50 + 40 * sin(extract(epoch FROM g) / 86400.0 + d),
       30 + 20 * cos(extract(epoch FROM g) / 43200.0 + d)
FROM generate_series(date_trunc('day', now()) - interval '45 days',
                     date_trunc('day', now()),
                     interval '1 hour') AS g,
     generate_series(1, 4) AS d;

-- Compression: configured + a mixed compressed/uncompressed state
-- (chunks older than 14 days compressed now; the policy keeps up
-- from here).
ALTER TABLE metrics SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'device_id',
    timescaledb.compress_orderby   = 'time DESC'
);
SELECT count(*) AS chunks_compressed_at_seed FROM (
    SELECT compress_chunk(c)
    FROM show_chunks('metrics', older_than => interval '14 days') AS c
) AS s;
SELECT add_compression_policy('metrics', compress_after => interval '14 days');

-- Retention: long enough that demo data never disappears mid-demo.
SELECT add_retention_policy('metrics', drop_after => interval '365 days');

-- Continuous aggregate + refresh policy; refreshed once at seed time
-- so the materialization hypertable has data and job_stats records a
-- success before the first scheduled run.
CREATE MATERIALIZED VIEW metrics_daily
WITH (timescaledb.continuous) AS
SELECT time_bucket('1 day', time) AS day,
       device_id,
       avg(cpu) AS avg_cpu,
       max(cpu) AS max_cpu,
       avg(mem) AS avg_mem
FROM metrics
GROUP BY 1, 2
WITH NO DATA;

SELECT add_continuous_aggregate_policy('metrics_daily',
    start_offset      => interval '30 days',
    end_offset        => interval '1 hour',
    schedule_interval => interval '1 hour');

CALL refresh_continuous_aggregate('metrics_daily', NULL, now() - interval '1 hour');

-- ---------------------------------------------------------------
-- events: integer-dimension hypertable (exercises the
-- range_*_integer columns and the creation-time chunk ordering).
-- 10 chunks of 86400 "ticks" each.
-- ---------------------------------------------------------------
CREATE TABLE events (
    ts      bigint NOT NULL,
    kind    text   NOT NULL,
    payload int    NOT NULL
);
SELECT create_hypertable('events', 'ts', chunk_time_interval => 86400);

INSERT INTO events (ts, kind, payload)
SELECT t, (ARRAY['create', 'update', 'delete'])[1 + (t / 3600) % 3], t % 100
FROM generate_series(0, 863999, 3600) AS t;

-- ---------------------------------------------------------------
-- legacy_measurements: a plain, append-mostly, time-keyed table —
-- deliberately NOT a hypertable. This is the exhibit for the future
-- TS-R008 "hypertable candidate" Analyzer rule (large table, leading
-- timestamptz column + time-leading btree, insert-only stats).
-- ---------------------------------------------------------------
CREATE TABLE legacy_measurements (
    recorded_at timestamptz      NOT NULL,
    sensor_id   int              NOT NULL,
    reading     double precision NOT NULL
);
CREATE INDEX legacy_measurements_recorded_at_idx
    ON legacy_measurements (recorded_at, sensor_id);

INSERT INTO legacy_measurements (recorded_at, sensor_id, reading)
SELECT g, s, random() * 100
FROM generate_series(date_trunc('day', now()) - interval '30 days',
                     date_trunc('day', now()),
                     interval '10 minutes') AS g,
     generate_series(1, 12) AS s;

-- ---------------------------------------------------------------
-- demo_failing_job: a user-defined action that fails on every run,
-- so timescaledb_job_stats_v1 shows total_failures > 0 and
-- timescaledb_information.job_errors accumulates rows (visible to
-- arq_monitor_owner; arq_monitor correctly sees none).
-- ---------------------------------------------------------------
CREATE PROCEDURE demo_failing_job(job_id int, config jsonb)
LANGUAGE plpgsql AS
$func$
BEGIN
    RAISE EXCEPTION 'tsdemo: deliberate failure (job_stats / job_errors demo)';
END
$func$;

SELECT alter_job(
    add_job('demo_failing_job', schedule_interval => interval '1 minute'),
    max_retries => 0);

-- Fresh planner stats for every collector that reads estimates.
RESET ROLE;
ANALYZE;
