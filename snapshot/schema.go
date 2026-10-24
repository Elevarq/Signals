package snapshot

import "github.com/elevarq/signals/internal/engineid"

// SchemaVersion identifies the format version of exported Elevarq Signals snapshots.
const SchemaVersion = "signals-snapshot.v1"

// Metadata describes the collector and collection context for an export.
type Metadata struct {
	SchemaVersion    string `json:"schema_version"`
	CollectorVersion string `json:"collector_version"`
	CollectorCommit  string `json:"collector_commit"`
	CollectedAt      string `json:"collected_at"`
	PGVersion        string `json:"pg_version"`
	// Engine declares the source database engine explicitly — one of the
	// engineid constants. Additive and backward-compatible (engine-identity.md,
	// INV-EID-01/02): omitted for PostgreSQL snapshots (which keep PGVersion),
	// populated for non-PostgreSQL engines. Always from a positive signal,
	// never inferred downstream.
	Engine string `json:"engine,omitempty"`
	// EngineVersion is the engine's own version for non-PostgreSQL engines
	// (e.g. the SingleStore @@memsql_version). Omitted for PostgreSQL, whose
	// version stays in PGVersion.
	EngineVersion string     `json:"engine_version,omitempty"`
	Target        TargetInfo `json:"target"`
}

// TargetInfo identifies the PostgreSQL target that was collected from.
type TargetInfo struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	DBName string `json:"dbname"`
}

// SetEngineIdentity applies a resolved engine identity to the metadata while
// preserving PostgreSQL's historical snapshot shape (engine-identity.md):
//
//   - PostgreSQL (or an empty engine) leaves Engine/EngineVersion empty, so the
//     marshaled snapshot stays byte-compatible with signals-snapshot.v1 and the
//     PostgreSQL version continues to live in PGVersion (INV-EID-01/02).
//   - A non-PostgreSQL engine records Engine + EngineVersion.
//
// The engine fields are additive and never remove or alter any existing v1
// field.
func (m *Metadata) SetEngineIdentity(engine, version string) {
	if engine == "" || engine == engineid.EnginePostgreSQL {
		return
	}
	m.Engine = engine
	m.EngineVersion = version
}
