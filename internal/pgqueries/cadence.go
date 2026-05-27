package pgqueries

import "time"

// LastRunMap tracks the last successful execution time per query ID.
type LastRunMap map[string]time.Time

// SelectDue returns queries where now - lastRun >= cadence (or never run).
// No catch-up: a query that missed multiple intervals still runs only once.
func SelectDue(now time.Time, queries []QueryDef, lastRuns LastRunMap) []QueryDef {
	var due []QueryDef
	for _, q := range queries {
		last, ok := lastRuns[q.ID]
		if !ok || now.Sub(last) >= q.Cadence.Duration() {
			due = append(due, q)
		}
	}
	return due
}
