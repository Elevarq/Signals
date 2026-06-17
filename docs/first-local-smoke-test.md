# First Local Smoke Test Report

Date: 2026-03-14
Target: PostgreSQL 18.1 on localhost:54318

## Configuration

```yaml
# test-config.yaml
env: dev
signals:
  poll_interval: 5m
  retention_days: 30
  log_level: info
  target_timeout: 30s
  query_timeout: 10s
targets:
  - name: local-test
    host: localhost
    port: 54318
    dbname: postgres
    user: postgres
    sslmode: disable
    enabled: true
database:
  path: /tmp/arq-signals-smoke.db
  wal: true
api:
  listen_addr: "127.0.0.1:18081"
```

Environment variables:
```
ARQ_ALLOW_INSECURE_PG_TLS=true
ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true
ARQ_SIGNALS_API_TOKEN=smoke-dev-local-only-replace-in-prod-32chars
```

## Commands used

```bash
# Build
go build -o /tmp/signals-bin ./cmd/signals

# Start
ARQ_ALLOW_INSECURE_PG_TLS=true \
ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true \
ARQ_SIGNALS_API_TOKEN=smoke-dev-local-only-replace-in-prod-32chars \
/tmp/signals-bin --config test-config.yaml

# Check status
curl -s -H "Authorization: Bearer smoke-dev-local-only-replace-in-prod-32chars" \
  http://127.0.0.1:18081/status

# Export
curl -s -o snapshot.zip -H "Authorization: Bearer smoke-dev-local-only-replace-in-prod-32chars" \
  http://127.0.0.1:18081/export
```

## Results

### Connection: PASS

PostgreSQL 18.1 connected successfully. Detected version:
```
PostgreSQL 18.1 (Postgres.app) on aarch64-apple-darwin23.6.0
```

### Safety checks: PASS (with expected bypass)

The `postgres` role has superuser, replication, and bypassrls
attributes. These were correctly detected as hard failures and
bypassed via `ARQ_SIGNALS_ALLOW_UNSAFE_ROLE=true`:

```
UNSAFE MODE: bypassing safety checks — not recommended for production
  bypassed_checks=[
    role "postgres" has superuser attribute (rolsuper=true),
    role "postgres" has replication attribute (rolreplication=true),
    role "postgres" has bypassrls attribute (rolbypassrls=true)
  ]
```

### Collection: PASS

11 of 12 registered queries executed successfully (pg_stat_statements
excluded — extension not installed). Collection completed in 64ms.

### Snapshot: PASS

Snapshot stored in SQLite. Size: 68,335 bytes.

### Export: PASS

ZIP archive produced with 5 files:
- metadata.json (565 bytes)
- snapshots.ndjson (68,577 bytes)
- query_catalog.json (1,708 bytes)
- query_runs.ndjson (3,847 bytes)
- query_results.ndjson (70,064 bytes)

### Credential safety: PASS

No actual passwords or credentials in the export. The words
"password_encryption" and "password_warnings" appear as PostgreSQL
setting *names* from `pg_settings` — these are configuration parameter
names, not credential values.

### Unsafe mode metadata: PASS

Export metadata correctly records:
```json
{
  "unsafe_mode": true,
  "unsafe_reasons": [
    "role has superuser attribute (rolsuper=true)",
    "role has replication attribute (rolreplication=true)",
    "role has bypassrls attribute (rolbypassrls=true)"
  ]
}
```

### Status endpoint: PASS

Returns target info without secret_type or secret_ref fields.

## Bug found and fixed

### NULL payload on zero-row query results

**Problem:** When a query returns zero rows, `EncodeNDJSON` returned
a nil byte slice. The `query_results.payload` column has a NOT NULL
constraint, causing the batch insert to fail for the entire set of
results.

**Fix:** Added nil-to-empty-slice guard in `EncodeNDJSON`:
```go
if raw == nil {
    raw = []byte{} // ensure non-nil for NOT NULL constraint
}
```

**Impact:** Without the fix, query_runs and query_results were not
persisted, though the legacy snapshot was still stored. After the fix,
all data is correctly persisted.

## Documentation mismatches discovered

None. The safety model, API responses, export structure, and
configuration behavior all matched documentation.
