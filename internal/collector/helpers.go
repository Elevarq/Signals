package collector

// parsePGMajorVersion and detectExtensions were retired by R081 in
// favour of pgqueries.Discover, which performs the same probes (and
// more) inside a single transaction with proper error handling.

// populateSnapshotField maps query results into the SnapshotData struct fields
// for backward compatibility with the monolithic snapshot format.
func populateSnapshotField(data *SnapshotData, queryID string, rows []map[string]any) {
	switch queryID {
	case "pg_version_v1":
		if len(rows) > 0 {
			if v, ok := rows[0]["version"]; ok {
				if s, ok := v.(string); ok {
					data.Version = s
				}
			}
		}
	case "pg_settings_v1":
		data.Settings = rows
	case "pg_stat_activity_v1":
		data.Activity = rows
	case "pg_stat_database_v1":
		data.Database = rows
	case "pg_stat_user_tables_v1":
		data.UserTables = rows
	case "pg_stat_user_indexes_v1":
		data.UserIndexes = rows
	case "pg_statio_user_tables_v1":
		data.StatioTables = rows
	case "pg_statio_user_indexes_v1":
		data.StatioIndexes = rows
	case "pg_stat_statements_v1":
		data.StatStatements = rows
	}
}
