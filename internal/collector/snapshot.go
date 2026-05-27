package collector

import "encoding/json"

// SnapshotData is the JSON payload stored per collection cycle per target.
type SnapshotData struct {
	Version        string           `json:"version"`
	Settings       []map[string]any `json:"settings"`
	Activity       []map[string]any `json:"activity"`
	Database       []map[string]any `json:"database"`
	UserTables     []map[string]any `json:"user_tables"`
	UserIndexes    []map[string]any `json:"user_indexes"`
	StatioTables   []map[string]any `json:"statio_tables"`
	StatioIndexes  []map[string]any `json:"statio_indexes"`
	StatStatements []map[string]any `json:"stat_statements,omitempty"`
}

// MarshalPayload serializes snapshot data to JSON.
func MarshalPayload(data *SnapshotData) (json.RawMessage, error) {
	return json.Marshal(data)
}
