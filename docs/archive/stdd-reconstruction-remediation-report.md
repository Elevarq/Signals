# STDD Reconstruction Remediation Report

Date: 2026-03-14

## Changes made

### New requirements added (10)

| ID | Summary | Category |
|----|---------|----------|
| R027 | Configuration via YAML + env vars | Configuration |
| R028 | Config file search order | Configuration |
| R029 | Single-target container mode | Configuration |
| R030 | Config validation at startup | Configuration |
| R031 | Initial forced collection at startup | Collection cycle |
| R032 | Overlap prevention between cycles | Collection cycle |
| R033 | Concurrent multi-target with bounded parallelism | Collection cycle |
| R034 | Commit failure blocks downstream persistence | Data integrity |
| R035 | Export metadata contract (unsafe_mode, unsafe_reasons) | Export |
| R036 | Persistence guarantees (atomic writes, retention, migration) | Persistence |

### New invariants added (2)
- INV-SIGNALS-08: Collection cycles must not overlap
- INV-SIGNALS-09: Transaction commit failure prevents persistence

### New failure conditions added (2)
- FC-06: Transaction commit failure → abort success path
- FC-07: Role safety check failure → block collection

### New acceptance test cases added (7)
- TC-SIG-037: Initial forced collection
- TC-SIG-038: Overlap prevention
- TC-SIG-039: Partial target failure
- TC-SIG-040: Configuration file loading with env override
- TC-SIG-041: Status endpoint excludes credentials
- TC-SIG-042: Export metadata contains specific unsafe reasons
- TC-SIG-043: Retention cleanup

### Acceptance tests rewritten for language neutrality (8)
- TC-SIG-005: Removed `pgqueries.All()` → "the catalog contains at least 9 entries"
- TC-SIG-006: Removed `[]map[string]any` → "3 result rows with mixed data types"
- TC-SIG-007: Removed `[]map[string]any` → "100+ result rows totaling >4KB"
- TC-SIG-010: Removed "Go imports" and `go vet` → "no source file depends on..."
- TC-SIG-013: Removed "pgx" reference → "PostgreSQL connections"
- TC-SIG-020: Removed `FilterParams{...}` → "PostgreSQL major version 14, no pg_stat_statements extension"
- TC-SIG-021: Removed `SelectDue` → "schedule queries with mixed cadences"
- TC-SIG-036: Removed `tx.Commit`, `InsertQueryRunBatch`, `InsertSnapshot` → "transaction commit returns an error", "no query results, snapshots, or success events are persisted"

### Appendices created (2)
- Appendix A: API Contract — endpoint schemas, auth, rate limiting, response formats, export metadata schema
- Appendix B: Configuration Schema — YAML schema, env var table, container mode, credential sources, TLS validation

### INV-SIGNALS-02 rewritten
- Before: "Adding a collector requires only a `Register()` call" (Go-specific)
- After: "Adding a collector requires only registering it with the catalog at startup" (language-neutral)

## Self-check: Can each subsystem be rebuilt without reading Go code?

### Configuration loading
**Before:** INSUFFICIENT — no requirement at all
**After:** SUFFICIENT — R027-R030 define the YAML schema, env var precedence, search order, container mode, and validation rules. Appendix B provides the complete schema with all fields, defaults, and variable names.

### API
**Before:** INSUFFICIENT — R011 listed 4 endpoints with no contracts
**After:** SUFFICIENT — R011 references Appendix A which defines exact response schemas, authentication, rate limiting, token comparison, error responses, and field exclusions.

### Collection cycle
**Before:** PARTIAL — R012/R015 defined timeouts and cadence but not cycle mechanics
**After:** SUFFICIENT — R031-R033 define the initial forced run, overlap prevention, and concurrent multi-target behavior with bounded parallelism.

### Export metadata
**Before:** PARTIAL — R005 defined basic metadata fields; R026 mentioned unsafe_mode
**After:** SUFFICIENT — R035 references the export metadata schema in Appendix A which defines all fields including unsafe_mode and unsafe_reasons.

### Data integrity
**Before:** NOT ADDRESSED — commit failure was only a test case (TC-SIG-036)
**After:** SUFFICIENT — R034 explicitly requires commit failure to block persistence. INV-SIGNALS-09 makes this an invariant. FC-06 defines the failure behavior.

### Persistence
**Before:** INSUFFICIENT — mentioned SQLite in scope but no requirements
**After:** SUFFICIENT — R036 defines persistence guarantees (atomic writes, retention cleanup, schema migration, stable instance ID) without mandating a specific storage engine.

### Safety model
**Before:** SUFFICIENT — unchanged (R017-R026 + INV-05/06/07)
**After:** SUFFICIENT — unchanged

### Snapshot format
**Before:** PARTIAL — R004-R006 good but export details missing
**After:** SUFFICIENT — R006 plus Appendix A export section

### CLI
**Before:** INSUFFICIENT — R010 listed 4 commands with no details
**After:** PARTIAL — R010 updated with flag and token configurability. Full CLI contract would benefit from its own appendix, but the current level is sufficient for a basic reimplementation.

## Updated portability scorecard

| Subsystem | Before | After |
|-----------|--------|-------|
| Config loading | INSUFFICIENT | SUFFICIENT |
| API | INSUFFICIENT | SUFFICIENT |
| Collection cycle | PARTIAL | SUFFICIENT |
| Snapshot export | PARTIAL | SUFFICIENT |
| Data integrity | NOT ADDRESSED | SUFFICIENT |
| Persistence | INSUFFICIENT | SUFFICIENT |
| Safety model | SUFFICIENT | SUFFICIENT |
| PostgreSQL connection | PARTIAL | PARTIAL (credential re-resolution detail in Appendix B) |
| CLI | INSUFFICIENT | PARTIAL |
| Redaction/logging | PARTIAL | PARTIAL (redaction rules in R024, detailed in Appendix A) |

**Before: 1 sufficient, 4 partial, 4 insufficient**
**After: 7 sufficient, 3 partial, 0 insufficient**

## Remaining gaps

1. **CLI contract**: R010 defines the minimum commands but not the full flag set, output formats, or per-command timeouts. These could be added in a future Appendix C if needed.

2. **Connection pooling semantics**: Max connections, password re-resolution per connection, and pool lifecycle are partially covered by R016 (credentials re-read) and Appendix B (credential behavior), but an explicit pooling requirement would complete this.

3. **Logging format**: No requirement defines the structured log format (fields, levels, JSON option). This is partially covered by R030 (config validation) and Appendix B (log_level, log_json) but a dedicated logging requirement could be added.

## Conclusion

The STDD layer has moved from "partially sufficient" to "materially
sufficient" for language-independent reconstruction. A competent
engineer can now rebuild Arq Signals from the specification,
appendices, and acceptance tests without reading the Go implementation
for all major subsystems. The three remaining PARTIAL areas (CLI,
connection pooling, logging) are low-risk and can be addressed
incrementally.
