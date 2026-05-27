# STDD Sufficiency Audit — Language-Independent Reconstruction

Audit date: 2026-03-14
Question: Can Arq Signals be rebuilt from the STDD documents alone?

## 1. Executive Summary

The STDD layer defines the **what** of Arq Signals well — its purpose,
boundaries, safety model, and interfaces. However, it does not define
enough of the **how** to enable faithful reconstruction in another
language without reading the Go implementation. Approximately 148
distinct behaviors exist in the codebase. The 26 requirements cover the
most important ones but leave significant operational, sequencing, and
edge-case behavior unspecified.

**Verdict: STDD is partially sufficient.** The specification is strong
on safety constraints and boundaries but weak on operational mechanics,
data formats, configuration details, API contracts, and failure
handling.

## 2. Can Arq Signals Be Rebuilt from STDD Alone?

A competent engineer reading only the STDD documents could build a
system that:
- Connects to PostgreSQL and runs read-only queries
- Blocks unsafe roles (superuser, replication, bypassrls)
- Produces NDJSON output in a ZIP archive
- Exposes a basic HTTP API and CLI

But they would get wrong or miss:
- Connection pooling semantics and password re-resolution
- Collection cycle overlap protection
- The initial forced collection at startup
- The dual snapshot format (legacy + granular)
- Worker pool concurrency model
- Timeout cascade (query → target → lock → idle)
- API middleware (rate limiting, constant-time comparison)
- Export metadata exact structure (unsafe_mode, unsafe_reasons)
- Commit-failure handling and its effect on persistence
- /status response structure (which fields, which excluded)
- pgpass file parsing rules
- Retention cleanup behavior
- Configuration file search order and env var precedence
- Graceful shutdown sequence

## 3. Strengths in the Current STDD Layer

**Product boundaries are well defined.** Requirements R007-R009 clearly
state what Arq Signals must NOT do. The invariants (INV-01 through
INV-07) are strong, language-neutral constraints.

**Safety model is well specified.** R017-R026 define the role validation,
read-only enforcement, timeout behavior, credential redaction, and
unsafe override model with enough precision for reimplementation.

**Failure conditions are defined.** FC-01 through FC-05 cover the major
failure modes, though they miss commit failures (now in TC-SIG-036 but
not in the FC list).

**Snapshot contract is partially defined.** R004-R006 define the format
(NDJSON, ZIP, metadata.json), and the examples directory provides
concrete samples.

## 4. Gaps That Still Require Reading the Go Code

### 4.1 Collection Cycle Mechanics (not in any requirement)
- Overlap protection (TryLock prevents concurrent cycles)
- Initial forced collection at startup
- Cadence planner integration (SelectDue algorithm)
- Early exit when no queries are due
- Worker pool with bounded concurrency

### 4.2 Connection Management (partially in R001, details missing)
- Max 2 connections per target pool
- Password re-resolution on every new connection (BeforeConnect)
- Dedicated connection acquisition for safety validation
- Pool lifecycle (creation, caching by target name, cleanup on shutdown)

### 4.3 Data Persistence Sequencing (not in any requirement)
- Queries execute → commit transaction → batch insert runs/results →
  insert legacy snapshot → log event
- Commit failure blocks all downstream persistence (TC-SIG-036 exists
  but no spec requirement)
- Dual storage: granular query_runs + legacy monolithic snapshots

### 4.4 API Contract Details (R011 is too sparse)
- /status exact response schema (which fields, what types)
- /status excluded fields (secret_type, secret_ref)
- /health response format
- /collect/now response code (202) and body
- /export Content-Disposition header format
- Authentication: bearer token, constant-time comparison
- Rate limiting: 5 failures per 5 minutes per IP
- Middleware ordering
- Request ID generation

### 4.5 CLI Details (R010 is too sparse)
- arqctl subcommand structure (collect now is a subcommand of collect)
- Default API address (http://127.0.0.1:8081)
- Token from ARQ_SIGNALS_API_TOKEN env var
- Export --output flag with default filename pattern
- Per-command timeouts

### 4.6 Configuration Model (not in any requirement)
- YAML structure with named sections (signals, targets, api, database)
- Config file search order (/etc/arq/signals.yaml → ./signals.yaml)
- Environment variable naming convention (ARQ_SIGNALS_*)
- Single-target container mode via env vars
- Duration string parsing
- TLS validation logic (prod vs non-prod, AllowInsecurePgTLS)
- Config validation warnings (non-blocking)

### 4.7 Export Metadata Structure (partially in R005/R026)
- Exact fields: schema_version, instance_id, collector_version,
  collector_commit, collected_at, unsafe_mode, unsafe_reasons
- unsafe_reasons is populated dynamically from collector at export time
- Export file ordering within ZIP

### 4.8 Error Handling Patterns (partially in FC list)
- Permission denied detection (SQLSTATE 42501) with remediation hint
- Timeout distinction in logging (query vs target budget)
- Error redaction rules (messages containing "password" or "secret")
- DSN redaction for both URL and key=value formats

### 4.9 Local Storage Schema (not in any requirement)
- SQLite with WAL mode, busy_timeout=5000, foreign_keys=on
- Migration system (schema_migrations table, ordered filenames)
- Tables: meta, targets, snapshots, events, query_catalog, query_runs,
  query_results, schema_migrations
- Instance ID generation (16 random bytes, hex-encoded)

## 5. Go-Specific Leakage into the STDD Layer

### In specification.md
- **INV-SIGNALS-02**: "Adding a collector requires only a `Register()`
  call" — references a Go function pattern. A Python implementation
  might use decorators, a registry dict, or a plugin system.

### In acceptance-tests.md
- **TC-SIG-005**: References `pgqueries.All()` — Go function name
- **TC-SIG-006/007**: References `[]map[string]any` — Go type syntax
- **TC-SIG-010**: References "Go imports" and `go vet`
- **TC-SIG-020**: References `FilterParams{PGMajorVersion: 14, Extensions: []}` — Go struct literal
- **TC-SIG-036**: References `tx.Commit`, `InsertQueryRunBatch`,
  `InsertSnapshot` — Go method names

### In traceability.md
- Every row references Go test file names (`signals_conn_test.go`)
- Notes reference Go functions: `BuildConnConfig`, `LintQuery`,
  `WriteTo`, `SafetyResult`, `IsSafe()`, `BeginTx`, `RedactDSN`,
  `WithAllowUnsafeRole`, `SelectDue`
- Notes reference Go testing frameworks: `httptest`
- Notes reference Go-specific concepts: "AST scan for cobra
  subcommand definitions"

### Assessment
The specification itself is mostly language-neutral (good). The
acceptance tests and traceability are heavily Go-coupled (expected for
traceability, problematic for acceptance tests).

## 6. Missing Requirements / Invariants / Acceptance Cases

### Missing Requirements
| Gap | Priority | Why it matters |
|-----|----------|----------------|
| Collection cycle semantics (overlap, initial forced run, worker pool) | HIGH | Core operational behavior |
| Configuration model (YAML structure, env vars, search order, precedence) | HIGH | Cannot configure the system without this |
| API response contracts (status schema, excluded fields, response codes) | HIGH | Cannot build compatible clients |
| Local persistence model (SQLite schema, migrations, WAL) | MEDIUM | Storage is an architectural choice |
| Connection pooling behavior (max conns, password re-resolution) | MEDIUM | Affects security and reliability |
| Commit-failure handling as a requirement (only exists as TC-SIG-036) | MEDIUM | Data integrity guarantee |
| Export metadata complete field list | MEDIUM | Snapshot contract completeness |
| CLI structure and defaults | LOW | Implementable from README |
| Graceful shutdown behavior | LOW | Operational nicety |
| Retention/cleanup behavior | LOW | Storage management |

### Missing Invariants
- Commit failure must prevent downstream persistence
- Collection cycles must not overlap
- Connection pools must re-resolve credentials on each new connection
- /status must not expose credential source paths

### Missing Acceptance Cases
- TC for commit-failure blocking (exists as TC-SIG-036 but no spec req)
- TC for overlap protection
- TC for initial forced collection
- TC for /status response schema (exact fields)
- TC for /status excluded fields
- TC for config file search order
- TC for env var precedence over file config
- TC for export metadata complete field set

## 7. Portability Assessment by Subsystem

| Subsystem | Status | Why |
|-----------|--------|-----|
| **Config loading** | INSUFFICIENT | No requirement defines YAML structure, env var convention, search order, or single-target container mode. A Python dev would have to reverse-engineer the schema. |
| **PostgreSQL connection** | PARTIAL | R001 defines connection params. Missing: pooling, max conns, password re-resolution, application_name, pgpass parsing rules. |
| **Collector execution** | PARTIAL | R003/R012/R014/R015 define what to collect, timeouts, filtering, and cadence. Missing: cycle overlap protection, initial forced run, worker pool, budget cascade, early exit. |
| **Safety model** | SUFFICIENT | R017-R026 plus INV-05/06/07 fully define the role checks, read-only enforcement, timeout application, hard/soft distinction, credential redaction, unsafe override, and error messages. |
| **Snapshot export** | PARTIAL | R004-R006 define format and packaging. Missing: exact metadata fields (unsafe_mode, unsafe_reasons), file ordering, export filtering (time range, target_id). |
| **API** | INSUFFICIENT | R011 lists 4 endpoints but does not define response schemas, status codes, auth mechanism, rate limiting, or field exclusions. |
| **CLI** | INSUFFICIENT | R010 lists 4 commands but does not define flags, defaults, subcommand structure, or output format. |
| **Local persistence** | INSUFFICIENT | No requirement mentions SQLite, WAL, migrations, or schema. These are architectural choices but critical for interoperability. |
| **Redaction/logging** | PARTIAL | R024 says "no secrets in logs/API/export" and R025 says "actionable errors." Missing: specific redaction rules, log format, log levels. |

**Overall: 1 SUFFICIENT, 4 PARTIAL, 4 INSUFFICIENT.**

## 8. Recommendations

### Priority 1: Add configuration model requirement
Define the YAML structure, supported sections, env var naming
convention, precedence rules, and config file search order. This is
essential for any reimplementation.

### Priority 2: Define API response contracts
Add a requirement or appendix specifying the exact JSON response
schema for /health, /status, /collect/now, and /export. Include
status codes, auth mechanism, and the secret field exclusion policy.

### Priority 3: Define collection cycle semantics
Add requirements for: overlap protection (only one cycle at a time),
initial forced collection on startup, concurrent target collection
with bounded worker pool, and commit-failure blocking.

### Priority 4: Define export metadata contract
Specify the complete metadata.json field set as an appendix or
sub-requirement of R005. Include unsafe_mode and unsafe_reasons.

### Priority 5: Remove Go-specific language from acceptance tests
Rewrite TC-SIG-005/006/007/010/020/036 to describe expected behavior
without referencing Go types, function names, or package structure.
Example: "The query catalog shall contain at least 9 entries" instead
of "`pgqueries.All()` returns >= 9 entries."

### Priority 6: Add local persistence requirements
If SQLite is a hard requirement (not just a current choice), state it.
If the storage backend is pluggable, define the storage interface.
Either way, define the migration model and schema expectations.

### Priority 7: Define CLI contract
Expand R010 to define subcommand structure, flags, default values,
and output format. Or add an appendix.

### Priority 8: Promote commit-failure handling to a requirement
TC-SIG-036 exists but has no backing requirement. Add a requirement
stating that transaction commit failure must prevent downstream
persistence of query results and snapshots.

## 9. Final Verdict

**STDD is partially sufficient for language-independent reconstruction.**

A Python developer reading the STDD documents would build a system
with the correct safety model, boundary enforcement, and query
collection behavior. But they would need the README, adoption guide,
and example config to understand configuration; they would need to
read the Go code to understand the API contract, collection cycle
mechanics, export metadata structure, and persistence model.

The specification is strongest where it matters most — the safety
model (9 requirements, 3 invariants) — and weakest on operational
infrastructure (config, API, CLI, storage). This is typical for a
first-generation STDD effort and can be addressed incrementally
without rearchitecting the documents.

**Estimated effort to reach full sufficiency:** 8 additional
requirements, 2 appendices (API contract, config schema), rewrite of
6 acceptance tests to remove Go syntax. Approximately 1-2 working
sessions.
