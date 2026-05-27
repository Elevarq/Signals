// FDW row post-processor.
//
// The four FDW collectors (fdw_wrappers_v1, fdw_servers_v1,
// fdw_user_mappings_v1, fdw_foreign_tables_v1) emit a `text[]`
// column containing libpq-style `key=value` option entries. Some
// of those values are credentials (passwords, tokens, private
// keys). This post-processor runs after queryToMaps and BEFORE the
// rows are encoded into NDJSON so cleartext credentials never
// reach the snapshot writer or the daemon's persisted store.
//
// The redaction logic itself lives in
// internal/pgqueries/fdw_redact.go (RedactFDWOptions + the
// fdwRedactPatterns list). This file is the wiring: it knows which
// collector ID owns which option column, and applies the redactor
// in place on the rows.
//
// Spec: specifications/collectors/fdw_*_v1.md REDACT-R001..R004.

package collector

import (
	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// fdwOptionColumnByID maps the FDW collector's logical ID to the
// column name in its row whose value is a `text[]` of libpq-style
// option entries needing redaction. A collector ID not in this map
// is left untouched (the post-processor is a no-op for every other
// collector in the registry).
var fdwOptionColumnByID = map[string]string{
	"fdw_wrappers_v1":       "fdw_options",
	"fdw_servers_v1":        "server_options",
	"fdw_user_mappings_v1":  "mapping_options",
	"fdw_foreign_tables_v1": "foreign_table_options",
}

// redactFDWRowsIfNeeded mutates rows in place when queryID names a
// known FDW collector. The targeted option column has its `text[]`
// value replaced with a `map[string]string` whose sensitive entries
// carry the literal "<redacted>". Encoding to NDJSON downstream
// preserves the map shape; the analyzer ingests it as a JSON object
// (was a JSON array of strings; the move to map is documented in
// the collector specs and pinned by the analyzer-side ingestion
// test).
//
// NOTE: this returns *no error*. Defensive type assertions handle
// (a) rows missing the option column (should not happen for the
// collectors above, but if a future override drops the column the
// post-processor degrades gracefully) and (b) rows whose option
// value is something other than the `[]any` shape pgx returns for
// `text[]` (also defensive — the `text[]` cast in the SQL
// guarantees this in practice).
func redactFDWRowsIfNeeded(queryID string, rows []map[string]any) {
	col, isFDW := fdwOptionColumnByID[queryID]
	if !isFDW {
		return
	}
	for _, row := range rows {
		raw, present := row[col]
		if !present || raw == nil {
			continue
		}
		// pgx renders Postgres `text[]` as []any with each element a
		// string. Walk it; bail out gracefully on any unexpected shape.
		arr, ok := raw.([]any)
		if !ok {
			continue
		}
		opts := make([]string, 0, len(arr))
		for _, v := range arr {
			if s, ok := v.(string); ok {
				opts = append(opts, s)
			}
		}
		row[col] = pgqueries.RedactFDWOptions(opts)
	}
}
