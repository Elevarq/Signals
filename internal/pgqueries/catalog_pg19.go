package pgqueries

// catalog_pg19.go — placeholder for PostgreSQL 19.
//
// PG 19 is **experimental** and not yet released as of this build
// (2026-04). The file exists so the per-major catalog layout is
// future-ready: when PG 19's stable schema is known, register the
// version-specific overrides here exactly as catalog_pg18.go does.
//
// Until then, the collector falls back to the highest supported
// catalog (PG 18) when it discovers a target running PG 19, and emits
// a startup warning so operators see the experimental status. See
// IsExperimentalMajor in discovery.go.

func init() {
	// no overrides; PG 19 SQL is undefined in this build.
}
