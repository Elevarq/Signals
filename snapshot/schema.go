package snapshot

// SchemaVersion identifies the format version of exported Arq Signals snapshots.
const SchemaVersion = "arq-snapshot.v1"

// Metadata describes the collector and collection context for an export.
type Metadata struct {
	SchemaVersion    string     `json:"schema_version"`
	CollectorVersion string     `json:"collector_version"`
	CollectorCommit  string     `json:"collector_commit"`
	CollectedAt      string     `json:"collected_at"`
	PGVersion        string     `json:"pg_version"`
	Target           TargetInfo `json:"target"`
}

// TargetInfo identifies the PostgreSQL target that was collected from.
type TargetInfo struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	DBName string `json:"dbname"`
}
