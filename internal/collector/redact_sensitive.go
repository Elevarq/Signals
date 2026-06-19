package collector

import "github.com/elevarq/signals/internal/pgqueries"

// redactHighSensitivityColumnsIfNeeded implements the R075 (revised
// 2026-05, issue #6) redact path: when the daemon-wide
// high-sensitivity gate is closed AND the collector declares
// SensitiveColumns, the named columns are set to nil in every row
// before NDJSON encoding/persistence. Non-sensitive columns survive so
// consumers retain the collector's diagnostic value (pid, wait_event,
// txn_age_seconds, waiting_seconds, ...).
//
// No-op when high-sensitivity is enabled (collect-everything default)
// or when SensitiveColumns is empty (skip-path collectors, which
// Filter has already dropped from the eligible set; this helper is
// still safe to call on them).
//
// Mirrors the same post-query, pre-encode pattern used by
// redactFDWRowsIfNeeded for FDW credential redaction.
func redactHighSensitivityColumnsIfNeeded(q pgqueries.QueryDef, rows []map[string]any, highSensitivityEnabled bool) {
	if highSensitivityEnabled || len(q.SensitiveColumns) == 0 {
		return
	}
	for _, row := range rows {
		for _, col := range q.SensitiveColumns {
			if _, ok := row[col]; ok {
				row[col] = nil
			}
		}
	}
}
