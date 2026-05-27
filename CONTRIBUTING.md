# Contributing to Arq Signals

Thank you for your interest in contributing to Arq Signals.

## Scope

Arq Signals is the open-source PostgreSQL diagnostic signal collector. It
collects data. It does not analyze, score, or interpret data. Contributions
must stay within this boundary.

**In scope:**
- New SQL collectors for PostgreSQL diagnostic views
- Bug fixes in collection, export, or scheduling logic
- Improvements to the CLI or API
- Documentation improvements
- Test coverage improvements
- Performance optimizations for collection

**Out of scope (these belong in arq-analyzer):**
- Analysis, scoring, or grading logic
- LLM or AI integration
- Recommendations or remediation guidance
- Dashboard or web UI features

## How to contribute

### Reporting issues

Open an issue on GitHub with:
- What you expected to happen
- What actually happened
- Steps to reproduce
- PostgreSQL version and Arq Signals version

### Submitting changes

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/my-change`)
3. Make your changes
4. Add or update tests
5. Run `go test ./...` and verify all tests pass
6. Run `go vet ./...` and fix any issues
7. Submit a pull request

### Adding a new collector

To add a new SQL collector:

1. Create or edit a file in `internal/pgqueries/`
2. Add an `init()` function that calls `Register()` with your `QueryDef`
3. The SQL must be a read-only SELECT or WITH query
4. Set appropriate `Cadence`, `RetentionClass`, and `Timeout`
5. If the query requires an extension, set `RequiresExtension`
6. If the query requires a minimum PG version, set `MinPGVersion`
7. Add tests verifying the query is registered and passes linting

Example:

```go
func init() {
    Register(QueryDef{
        ID:             "pg_my_view_v1",
        Category:       "custom",
        SQL:            "SELECT * FROM pg_my_view",
        ResultKind:     ResultRowset,
        RetentionClass: RetentionMedium,
        Timeout:        10 * time.Second,
        Cadence:        Cadence15m,
    })
}
```

The linter will reject your query at startup if it contains DDL, DML, or
dangerous functions. This is intentional — all collectors must be read-only.

## Safety requirements

All contributions must preserve the production safety guarantees:

- **Read-only only.** Every SQL query must pass the static linter
  (SELECT/WITH only, no DDL, DML, or dangerous functions).
- **No credentials in output.** Passwords must never appear in logs,
  exports, API responses, or stored data.
- **No external network calls.** Arq Signals must not phone home,
  upload telemetry, or contact external services.
- **Fail-closed for unsafe roles.** Superuser, replication, and
  bypassrls roles must be blocked unless explicitly overridden.

If your change affects the safety model, update the relevant STDD
artifacts in `features/arq-signals/` and add corresponding tests.

## Code style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep functions short and focused
- Add comments only where the logic is not self-evident
- Use table-driven tests where appropriate

## License

By contributing to Arq Signals, you agree that your contributions will be
licensed under the BSD-3-Clause license.
