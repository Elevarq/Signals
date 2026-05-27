package pgqueries

// catalog_pg16.go — version-specific overrides for PostgreSQL 16.
// See catalog_pg14.go for the contract.
func init() {
	// #210 — real PG 14+ session/timing columns for pg_stat_database_v1.
	RegisterOverride(16, "pg_stat_database_v1", pgStatDatabaseV14SQL)
}
