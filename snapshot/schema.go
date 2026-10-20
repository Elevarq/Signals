package snapshot

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
	// engineid constants ("postgresql", "mysql", "singlestore"). Additive
	// and backward-compatible: omitted for existing PostgreSQL snapshots
	// (which continue to carry PGVersion), populated for non-PostgreSQL
	// engines. Engine identity is always declared from a positive signal,
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
