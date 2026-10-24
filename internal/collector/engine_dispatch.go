package collector

import "github.com/elevarq/signals/internal/config"

// Backend identifies which collection backend a target uses. The existing
// collection cycle (collectTarget) is the PostgreSQL/pgx backend; a
// non-PostgreSQL engine is served by the MySQL-wire path and must never be
// connected with pgx (INV-ECD-01).
//
// Specification: specifications/engine-collection-dispatch.md
// (ECD-R001/R002, INV-ECD-01/02).
type Backend int

const (
	// BackendPostgreSQL is the existing pgx collection path.
	BackendPostgreSQL Backend = iota
	// BackendMySQLWire is the MySQL-wire path (MySQL + SingleStore).
	BackendMySQLWire
)

// SelectBackend maps a target's effective engine to its collection backend.
// Empty/postgresql → BackendPostgreSQL; mysql/mariadb/singlestore → BackendMySQLWire.
func SelectBackend(t config.TargetConfig) Backend {
	switch t.EffectiveEngine() {
	case config.EngineMySQL, config.EngineMariaDB, config.EngineSingleStore:
		return BackendMySQLWire
	default:
		return BackendPostgreSQL
	}
}
