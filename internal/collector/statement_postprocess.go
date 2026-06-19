// Statement-text secret post-processor.
//
// Some collectors expose raw SQL statement text that can contain
// structured credential literals (e.g. pg_stat_statements_v1's `query`
// column carries un-normalized CREATE/ALTER ROLE … PASSWORD '…'). This
// post-processor runs after queryToMaps and BEFORE the rows are encoded
// into NDJSON, so cleartext credentials never reach the snapshot writer
// or the daemon's persisted store (REDACT-R001..R003, #188 /
// INV-SIGNALS-07).
//
// The redaction logic lives in internal/pgqueries/statement_redact.go
// (RedactStatementSecrets). This file is the wiring: which collector ID
// owns which statement-text column.

package collector

import (
	"github.com/elevarq/signals/internal/pgqueries"
)

// statementTextColumnByID maps a collector ID to the row column whose
// value is raw SQL statement text needing structured-credential
// redaction. Only pg_stat_statements_v1 exposes such text today; the map
// keeps the wiring explicit and a no-op for every other collector.
var statementTextColumnByID = map[string]string{
	"pg_stat_statements_v1": "query",
}

// redactStatementSecretsIfNeeded mutates rows in place when queryID names
// a collector with a statement-text column, masking structured credential
// literals before the rows are persisted/exported. Unconditional — it does
// not consult the high-sensitivity gate, because a credential must never
// be exportable (INV-SIGNALS-07). No-op for every other collector.
func redactStatementSecretsIfNeeded(queryID string, rows []map[string]any) {
	col, ok := statementTextColumnByID[queryID]
	if !ok {
		return
	}
	for _, row := range rows {
		if s, ok := row[col].(string); ok {
			row[col] = pgqueries.RedactStatementSecrets(s)
		}
	}
}
