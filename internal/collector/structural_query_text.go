package collector

import (
	"unicode/utf8"

	"github.com/elevarq/signals/internal/pgqueries"
)

// Structural query text (#647, specifications/structural-query-text.md).
//
// Opt-in, secure-default-OFF, single-column retention of the
// server-normalized DIGEST_TEXT for the MySQL-wire family. The server
// normalizes literals to `?` before Signals reads the column, so the
// retained value is a query *shape* and does not intentionally expose
// runtime literal values (INV-SQT-01, qualified adversarially per
// engine/version). It remains sensitive structural evidence because it
// exposes schema/table/column identifiers, hence the explicit opt-in.

// structuralQueryTextMaxBytes is the fixed per-row byte cap on a retained
// DIGEST_TEXT (SQT-R003). Deliberately a constant, not a config knob, for
// the POC — revisit from empirical evidence later if needed.
const structuralQueryTextMaxBytes = 4096

// structuralQueryTextMarker is appended to a value truncated at the cap so a
// human reading the NDJSON sees the cut. The authoritative, machine-visible
// signal is the digest_text_truncated field, not this marker (SQT-R003/R007).
const structuralQueryTextMarker = "…"

// digestTextTruncatedField is the additive, machine-visible per-row flag that
// states whether the retained DIGEST_TEXT was cut to the cap (SQT-R007). A
// consumer MUST NOT treat a truncated value as complete structural evidence.
const digestTextTruncatedField = "digest_text_truncated"

// structuralQueryTextColumns maps a digest collector ID to the single
// server-normalized column this capability may surface. Only these collectors
// are affected; every other collector's SensitiveColumns stay governed solely
// by the generic high-sensitivity switch (SQT-R002 / INV-SQT-04). The digest
// collector is shared across MySQL / MariaDB / SingleStore, so MariaDB reuses
// the MySQL path by construction — no divergent structural path (SQT-R008).
var structuralQueryTextColumns = map[string]string{
	"mysql_statement_digests_v1": "DIGEST_TEXT",
}

// structuralQueryTextColumnFor returns the collector's structural column and
// whether it has one.
func structuralQueryTextColumnFor(q pgqueries.QueryDef) (string, bool) {
	col, ok := structuralQueryTextColumns[q.ID]
	return col, ok
}

// isStructuralQueryTextColumn reports whether col is the structural column for
// collector q — the column governed SOLELY by the structural opt-in, carved
// out of the generic high-sensitivity redaction (SQT-R001).
func isStructuralQueryTextColumn(q pgqueries.QueryDef, col string) bool {
	sc, ok := structuralQueryTextColumns[q.ID]
	return ok && sc == col
}

// truncateStructuralQueryText returns s bounded to structuralQueryTextMaxBytes
// on a valid UTF-8 boundary, and whether it was truncated. When truncated, the
// returned value reserves room for the marker and ends with it, so the result
// never exceeds the cap and is always valid UTF-8 (SQT-R003).
func truncateStructuralQueryText(s string) (string, bool) {
	if len(s) <= structuralQueryTextMaxBytes {
		return s, false
	}
	markerLen := len(structuralQueryTextMarker)
	limit := structuralQueryTextMaxBytes - markerLen
	if limit < 0 {
		limit = 0
	}
	// Back off to a rune boundary at or before limit.
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + structuralQueryTextMarker, true
}

// applyStructuralQueryTextBound bounds the retained structural column and
// stamps the machine-visible digest_text_truncated flag on every row, but
// ONLY when the capability is enabled and the collector carries a structural
// column. A no-op otherwise, so default/other-collector output is unchanged
// (INV-SQT-02). A null (or absent) source value stays null and is marked not
// truncated — nothing is fabricated.
func applyStructuralQueryTextBound(q pgqueries.QueryDef, rows []map[string]any, structuralQueryTextEnabled bool) {
	if !structuralQueryTextEnabled {
		return
	}
	col, ok := structuralQueryTextColumnFor(q)
	if !ok {
		return
	}
	for _, row := range rows {
		v, present := row[col]
		if !present {
			continue
		}
		s, isStr := v.(string)
		if !isStr || s == "" {
			// Null, non-string, or empty source: never fabricate, never
			// claim truncation.
			row[digestTextTruncatedField] = false
			continue
		}
		bounded, truncated := truncateStructuralQueryText(s)
		row[col] = bounded
		row[digestTextTruncatedField] = truncated
	}
}
