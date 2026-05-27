-- arq-signals monitoring role setup
-- This script runs automatically when PostgreSQL starts for the first time.

-- Create the monitoring role with login but no superuser privileges
CREATE ROLE arq_monitor WITH LOGIN PASSWORD 'monitor_pass';

-- Grant access to PostgreSQL statistics views
-- pg_monitor is available in PostgreSQL 10+
GRANT pg_monitor TO arq_monitor;

-- Enable query-level statistics (optional but recommended)
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Create a sample table so there is something to monitor
CREATE TABLE IF NOT EXISTS example_data (
    id SERIAL PRIMARY KEY,
    value TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT now()
);

-- Insert sample rows to generate some statistics
INSERT INTO example_data (value)
SELECT 'sample-' || generate_series(1, 100);

-- Run ANALYZE so pg_stat_user_tables has data
ANALYZE example_data;
