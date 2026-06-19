package tests

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/pgqueries"
)

// ---------------------------------------------------------------------------
// R081 catalog drift check (issue #74).
//
// For each registered collector ID, the resolved SQL across every
// supported PG major must declare the same set of output columns —
// the contract that downstream consumers (analyzer ingest, exports)
// depend on. If someone adds a column to the PG 18 variant of
// `pg_stat_io_v1` without updating PG 14..17, the output is silently
// inconsistent across exports from different majors. This test
// catches that at lint time.
//
// Approach:
//   1. For each collector, render its effective SQL for each
//      PGMajorVersion in the supported window.
//   2. Parse the SELECT list for top-level output columns. Collectors
//      follow the existing "no SELECT *" convention so every column
//      has an explicit `AS <name>` (verified by the no-select-star
//      tests).
//   3. Compare column sets across majors. Diverging sets fail the
//      test naming the collector and the majors involved.
//
// Allowlist for legitimate cross-version divergence is at the bottom
// of this file. Each entry has a citation.
// ---------------------------------------------------------------------------

func TestCatalogDriftAcrossPGMajors(t *testing.T) {
	// Render every collector at every supported major and group its
	// column sets by (id, major).
	majors := pgqueries.SupportedMajors

	allCollectors := pgqueries.All()
	if len(allCollectors) == 0 {
		t.Fatal("no collectors registered; this test cannot run")
	}

	for _, q := range allCollectors {
		if allowlistedDivergence[q.ID] {
			continue
		}

		var firstCols []string
		var firstMajor int
		for _, major := range majors {
			if q.MinPGVersion > 0 && major < q.MinPGVersion {
				continue
			}
			filtered := pgqueries.Filter(pgqueries.FilterParams{
				PGMajorVersion:         major,
				Extensions:             extensionsForCollector(q),
				HighSensitivityEnabled: true,
			})
			var resolved string
			for _, fq := range filtered {
				if fq.ID == q.ID {
					resolved = fq.SQL
					break
				}
			}
			if resolved == "" {
				continue // skipped on this major; not a drift case
			}
			cols := extractSelectListColumns(resolved)
			if firstCols == nil {
				firstCols = cols
				firstMajor = major
				continue
			}
			if !sameColumnSet(firstCols, cols) {
				t.Errorf("collector %q: column-set drift between PG %d %v and PG %d %v",
					q.ID, firstMajor, firstCols, major, cols)
			}
		}
	}
}

// extractSelectListColumns parses the top-level SELECT list of the
// SQL and returns the column aliases / names. It handles the
// "no SELECT *" forms used by every registered collector (verified
// by separate test) and the `WITH ...` CTE preamble.
//
// Heuristic: look for `AS <ident>` patterns at top level. Stops at
// the first top-level FROM keyword.
var (
	asPattern   = regexp.MustCompile(`(?i)\bAS\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	fromPattern = regexp.MustCompile(`(?i)\bFROM\b`)
)

func extractSelectListColumns(sql string) []string {
	// Strip leading WITH ... AS ( ... ) CTEs to focus on the final
	// SELECT list. We accept some over-broad matching — the
	// allowlist catches the cases where this isn't precise enough.
	selectIdx := strings.LastIndex(strings.ToUpper(sql), "SELECT")
	if selectIdx == -1 {
		return nil
	}
	body := sql[selectIdx+len("SELECT"):]
	if from := fromPattern.FindStringIndex(body); from != nil {
		body = body[:from[0]]
	}
	matches := asPattern.FindAllStringSubmatch(body, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, strings.ToLower(m[1]))
	}
	sort.Strings(out)
	return out
}

func sameColumnSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// extensionsForCollector returns the set of extensions a collector
// requires so Filter doesn't drop it on the extension gate during
// the drift check.
func extensionsForCollector(q pgqueries.QueryDef) []string {
	if q.RequiresExtension == "" {
		return nil
	}
	return []string{q.RequiresExtension}
}

// allowlistedDivergence lists collectors whose output column set
// legitimately differs across PG majors. Each entry must cite the
// spec or upstream change that justifies the divergence.
var allowlistedDivergence = map[string]bool{
	// pg_stat_io view added in PG 16; column names changed in PG 18
	// (read_bytes/write_bytes/extend_bytes replaced op_bytes). The
	// version-aware catalog normalises the LOGICAL column set per
	// major, but the SQL text legitimately differs.
	// See: specifications/collectors/pg_stat_io_v1.md
	//      features/signals/traceability.md (R081)
	"pg_stat_io_v1": true,

	// pg_stat_wal view existed pre-PG 14 but column shape changed in
	// PG 18 (wal_writes/wal_syncs replaced wal_write/wal_sync). Same
	// rationale as pg_stat_io_v1.
	// See: specifications/collectors/pg_stat_wal_v1.acceptance.md
	"pg_stat_wal_v1": true,

	// pg_stat_bgwriter / pg_stat_checkpointer split in PG 17.
	// pg_stat_bgwriter uses SELECT * for forward compatibility — the
	// regex-based extractor can't reason about it.
	// See: internal/pgqueries/catalog_diagnostics.go bgwriter_stats_v1
	"bgwriter_stats_v1": true,

	// checkpointer_stats_v1 also uses SELECT *.
	"checkpointer_stats_v1": true,

	// pg_stat_progress_vacuum reshaped its dead-tuple accounting in
	// PG 17: max_dead_tuples / num_dead_tuples were replaced by
	// max_dead_tuple_bytes / dead_tuple_bytes and the view added
	// num_dead_item_ids + indrelid. The base SQL and the PG 17+
	// override emit the same canonical column union; only the SQL
	// level NULL stubs flip per major (same rationale as
	// pg_stat_io_v1).
	// See: specifications/collectors/pg_stat_progress_family_v1.md
	"pg_stat_progress_vacuum_v1": true,

	// pg_stat_progress_copy added tuples_skipped in PG 17. Same
	// flip-the-NULL-stub pattern as above.
	"pg_stat_progress_copy_v1": true,
}

// Verifies the allowlist itself stays bounded — accidentally adding
// entries to it would erode the test's value. A future allowlist
// growth should be an explicit decision, not a forgotten exception.
func TestCatalogDriftAllowlistBounded(t *testing.T) {
	const maxAllowed = 6
	if len(allowlistedDivergence) > maxAllowed {
		t.Errorf("catalog-drift allowlist has grown to %d entries (max %d) — review whether each is still justified",
			len(allowlistedDivergence), maxAllowed)
	}
}

// Sanity: every allowlist entry must refer to a real registered
// collector. A typo here would silently disable the drift check
// for nothing.
func TestCatalogDriftAllowlistReferencesRealCollectors(t *testing.T) {
	registered := map[string]bool{}
	for _, q := range pgqueries.All() {
		registered[q.ID] = true
	}
	for id := range allowlistedDivergence {
		if !registered[id] {
			t.Errorf("catalog-drift allowlist references unknown collector %q", id)
		}
	}
}
