package singlestore

import "unicode/utf8"

// Structural query text (#647, specifications/structural-query-text.md).
//
// Opt-in, secure-default-OFF retention of the server-normalized DIGEST_TEXT for
// the MySQL-wire digest collectors (MySQL, MariaDB). The server normalizes
// literals to `?` before Signals reads the column, so the retained value is a
// query shape and does not intentionally expose runtime literal values
// (INV-SQT-01). The column a collector exposes under this opt-in is named by
// Collector.StructuralColumn; a collector with an empty StructuralColumn (every
// collector except the two digest collectors — notably SingleStore's
// singlestore_plancache_v1 / QUERY_TEXT) is never surfaced by this capability
// (SQT-R010).

// StructuralQueryTextMaxBytes is the fixed per-row byte cap on a retained
// structural column (SQT-R003). A constant for the POC, not a config knob.
const StructuralQueryTextMaxBytes = 4096

// StructuralQueryTextMarker is appended to a value truncated at the cap. The
// authoritative, machine-visible signal is DigestTextTruncatedField, not this
// marker (SQT-R003/R007).
const StructuralQueryTextMarker = "…"

// DigestTextTruncatedField is the additive per-row flag stating whether the
// retained structural column was cut to the cap (SQT-R007). A consumer MUST NOT
// treat a truncated value as complete structural evidence.
const DigestTextTruncatedField = "digest_text_truncated"

// truncateStructural returns s bounded to StructuralQueryTextMaxBytes on a valid
// UTF-8 boundary and whether it was truncated. A truncated result reserves room
// for the marker, ends with it, stays within the cap, and is valid UTF-8.
func truncateStructural(s string) (string, bool) {
	if len(s) <= StructuralQueryTextMaxBytes {
		return s, false
	}
	limit := StructuralQueryTextMaxBytes - len(StructuralQueryTextMarker)
	if limit < 0 {
		limit = 0
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + StructuralQueryTextMarker, true
}

// applyWireRedaction applies the high-sensitivity redaction and the #647
// structural carve-out to one collector's rows, in place.
//
//   - The structural column (c.StructuralColumn) is governed SOLELY by
//     structuralQueryText (decoupled from redactSensitive / the generic
//     high-sensitivity switch): kept — and bounded + flagged — only when the
//     opt-in is on, otherwise nilled. This is the sole gate for that column
//     (SQT-R001/R002).
//   - Every other SensitiveColumns entry is nilled when redactSensitive is true
//     (the existing SSC-R003 behaviour), untouched otherwise.
func applyWireRedaction(c Collector, rows []map[string]any, redactSensitive, structuralQueryText bool) {
	if len(c.SensitiveColumns) == 0 {
		return
	}
	for _, row := range rows {
		for _, col := range c.SensitiveColumns {
			if _, ok := row[col]; !ok {
				continue
			}
			if c.StructuralColumn != "" && col == c.StructuralColumn {
				if !structuralQueryText {
					row[col] = nil
					continue
				}
				if s, isStr := row[col].(string); isStr && s != "" {
					bounded, truncated := truncateStructural(s)
					row[col] = bounded
					row[DigestTextTruncatedField] = truncated
				} else {
					// null / non-string / empty source: never fabricate.
					row[DigestTextTruncatedField] = false
				}
				continue
			}
			if redactSensitive {
				row[col] = nil
			}
		}
	}
}
