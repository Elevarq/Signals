package collector

import (
	"sort"
	"strings"
	"time"

	"github.com/elevarq/arq-signals/internal/db"
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
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "timeout"
	default:
		return "execution_error"
	}
}
