package collector

import (
	"sort"
	"strings"
	"time"

	"github.com/elevarq/signals/internal/db"
)

// CollectorStatus records the execution outcome of a single collector.
//
// Specification: specifications/collector_status.md
type CollectorStatus struct {
	ID          string `json:"id"`
	Attempted   bool   `json:"attempted"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
	Detail      string `json:"detail"`
	RowCount    int    `json:"row_count"`
	DurationMS  int    `json:"duration_ms"`
	CollectedAt string `json:"collected_at"`
	// Cadence is the collector's expected run interval as a duration
	// string ("5m", "24h"); empty when the collector is not in the
	// registry. Freshness is one of "fresh", "stale", "never_run"
	// (R107) — empty for entries where freshness does not apply (e.g.
	// gated/skipped collectors). Both are populated by the export
	// builder, not by the daemon's live status path.
	Cadence   string `json:"cadence,omitempty"`
	Freshness string `json:"freshness,omitempty"`
}

// CollectorStatusFile is the top-level structure for collector_status.json.
type CollectorStatusFile struct {
	SchemaVersion string            `json:"schema_version"`
	TargetName    string            `json:"target_name,omitempty"`
	CollectedAt   string            `json:"collected_at"`
	Collectors    []CollectorStatus `json:"collectors"`
}

// Sort orders collectors by ID for deterministic output.
func (f *CollectorStatusFile) Sort() {
	sort.Slice(f.Collectors, func(i, j int) bool {
		return f.Collectors[i].ID < f.Collectors[j].ID
	})
}

// NewSuccessStatus creates a status entry for a successful collection.
func NewSuccessStatus(id string, rowCount, durationMS int, collectedAt time.Time) CollectorStatus {
	return CollectorStatus{
		ID:          id,
		Attempted:   true,
		Status:      "success",
		Reason:      "",
		Detail:      "",
		RowCount:    rowCount,
		DurationMS:  durationMS,
		CollectedAt: collectedAt.UTC().Format(time.RFC3339),
	}
}

// NewSkippedStatus creates a status entry for a skipped collector.
func NewSkippedStatus(id, reason, detail string) CollectorStatus {
	return CollectorStatus{
		ID:          id,
		Attempted:   false,
		Status:      "skipped",
		Reason:      reason,
		Detail:      detail,
		RowCount:    0,
		DurationMS:  0,
		CollectedAt: "",
	}
}

// NewFailedStatus creates a status entry for a failed collector.
func NewFailedStatus(id, reason, detail string, durationMS int, collectedAt time.Time) CollectorStatus {
	return CollectorStatus{
		ID:          id,
		Attempted:   true,
		Status:      "failed",
		Reason:      reason,
		Detail:      detail,
		RowCount:    0,
		DurationMS:  durationMS,
		CollectedAt: collectedAt.UTC().Format(time.RFC3339),
	}
}

// BuildStatusFromRuns constructs collector status entries from query
// run records. This is used to reconstruct per-target status from
// the query_runs table for target-scoped exports.
//
// The persisted run carries an explicit status (success / failed /
// skipped). Older rows with no explicit status are treated by the
// migration as success when error is empty and failed otherwise; this
// function uses the same fallback so it works against pre-migration
// data ingested by older tools.
func BuildStatusFromRuns(runs []db.QueryRun) []CollectorStatus {
	statuses := make([]CollectorStatus, 0, len(runs))

	for _, r := range runs {
		status := r.Status
		if status == "" {
			if r.Error != "" {
				status = "failed"
			} else {
				status = "success"
			}
		}

		switch status {
		case "skipped":
			statuses = append(statuses, CollectorStatus{
				ID:        r.QueryID,
				Attempted: false,
				Status:    "skipped",
				Reason:    r.Reason,
				Detail:    r.Error,
			})
		case "failed":
			reason := r.Reason
			if reason == "" {
				reason = classifyRunError(r.Error)
			}
			statuses = append(statuses, CollectorStatus{
				ID:          r.QueryID,
				Attempted:   true,
				Status:      "failed",
				Reason:      reason,
				Detail:      r.Error,
				DurationMS:  r.DurationMS,
				CollectedAt: r.CollectedAt,
			})
		default: // success
			statuses = append(statuses, CollectorStatus{
				ID:          r.QueryID,
				Attempted:   true,
				Status:      "success",
				RowCount:    r.RowCount,
				DurationMS:  r.DurationMS,
				CollectedAt: r.CollectedAt,
			})
		}
	}

	return statuses
}

// classifyRunError maps an error string to a reason category.
func classifyRunError(errMsg string) string {
	lower := strings.ToLower(errMsg)
	switch {
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "42501"):
		return "permission_denied"
	// R115: undefined table (42P01) / undefined function (42883) —
	// an extension surface the gates expected is absent at execution
	// time (upstream view removal, extension API schema not on the
	// collector role's search_path). Structured so operators and the
	// Analyzer completeness model can tell "object vanished" from a
	// generic execution error. Matched on the SQLSTATE token ONLY —
	// pgx error strings always embed it ("... (SQLSTATE 42P01)"). A
	// message-text match like "does not exist" would sweep in
	// unrelated classes (26000 pooler prepared-statement errors,
	// 42703 catalog-drift column errors, 3D000/28000 connection
	// errors). Placed before the timeout substring so a 42P01 whose
	// quoted identifier contains "timeout" cannot misroute.
	case strings.Contains(lower, "42p01") || strings.Contains(lower, "42883"):
		return "object_missing"
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "timeout"
	default:
		return "execution_error"
	}
}
