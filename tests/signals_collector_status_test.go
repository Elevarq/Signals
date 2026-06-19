package tests

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/collector"
)

// --- CollectorStatus struct ---

func TestCollectorStatusSuccess(t *testing.T) {
	s := collector.CollectorStatus{
		ID:          "pg_stat_user_tables_v1",
		Attempted:   true,
		Status:      "success",
		Reason:      "",
		Detail:      "",
		RowCount:    47,
		DurationMS:  12,
		CollectedAt: "2026-03-16T14:30:01Z",
	}
	if s.Status != "success" || s.Reason != "" {
		t.Error("success status should have empty reason")
	}
	if !s.Attempted {
		t.Error("success must be attempted")
	}
}

func TestCollectorStatusSkippedVersionUnsupported(t *testing.T) {
	s := collector.CollectorStatus{
		ID:        "pg_functions_v1",
		Attempted: false,
		Status:    "skipped",
		Reason:    "version_unsupported",
		Detail:    "Requires PostgreSQL 11+; connected to PostgreSQL 10.23",
	}
	if s.Attempted {
		t.Error("skipped must not be attempted")
	}
	if s.DurationMS != 0 {
		t.Error("skipped must have duration_ms=0")
	}
	if s.CollectedAt != "" {
		t.Error("skipped must have empty collected_at")
	}
}

func TestCollectorStatusSkippedExtensionMissing(t *testing.T) {
	s := collector.CollectorStatus{
		ID:     "pg_stat_statements_v1",
		Status: "skipped",
		Reason: "extension_missing",
		Detail: "pg_stat_statements extension is not installed",
	}
	if s.Reason != "extension_missing" {
		t.Errorf("reason: got %q", s.Reason)
	}
}

func TestCollectorStatusSkippedConfigDisabled(t *testing.T) {
	s := collector.CollectorStatus{
		ID:     "pg_views_definitions_v1",
		Status: "skipped",
		Reason: "config_disabled",
		Detail: "Collector disabled in signals.yaml",
	}
	if s.Reason != "config_disabled" {
		t.Errorf("reason: got %q", s.Reason)
	}
}

func TestCollectorStatusFailedPermissionDenied(t *testing.T) {
	s := collector.CollectorStatus{
		ID:          "vacuum_health_v1",
		Attempted:   true,
		Status:      "failed",
		Reason:      "permission_denied",
		Detail:      "permission denied for relation pg_stat_all_tables",
		DurationMS:  3,
		CollectedAt: "2026-03-16T14:30:03Z",
	}
	if !s.Attempted {
		t.Error("failed must be attempted")
	}
	if s.RowCount != 0 {
		t.Error("failed must have row_count=0")
	}
}

func TestCollectorStatusFailedExecutionError(t *testing.T) {
	s := collector.CollectorStatus{
		ID:        "pg_constraints_v1",
		Attempted: true,
		Status:    "failed",
		Reason:    "execution_error",
		Detail:    "column \"conkey\" does not exist",
	}
	if s.Reason != "execution_error" {
		t.Errorf("reason: got %q", s.Reason)
	}
}

func TestCollectorStatusFailedTimeout(t *testing.T) {
	s := collector.CollectorStatus{
		ID:        "pg_stats_v1",
		Attempted: true,
		Status:    "failed",
		Reason:    "timeout",
		Detail:    "query exceeded 30s timeout",
	}
	if s.Reason != "timeout" {
		t.Errorf("reason: got %q", s.Reason)
	}
}

func TestCollectorStatusPartial(t *testing.T) {
	s := collector.CollectorStatus{
		ID:         "login_roles_v1",
		Attempted:  true,
		Status:     "partial",
		Reason:     "permission_limited",
		Detail:     "some attributes restricted",
		RowCount:   12,
		DurationMS: 8,
	}
	if s.Status != "partial" {
		t.Errorf("status: got %q", s.Status)
	}
}

// --- JSON serialization ---

func TestCollectorStatusJSONShape(t *testing.T) {
	s := collector.CollectorStatus{
		ID:          "pg_stat_user_tables_v1",
		Attempted:   true,
		Status:      "success",
		Reason:      "",
		Detail:      "",
		RowCount:    47,
		DurationMS:  12,
		CollectedAt: "2026-03-16T14:30:01Z",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	_ = json.Unmarshal(data, &m)

	for _, key := range []string{"id", "attempted", "status", "reason", "detail", "row_count", "duration_ms", "collected_at"} {
		if _, ok := m[key]; !ok {
			t.Errorf("JSON must contain key %q", key)
		}
	}
}

func TestCollectorStatusJSONDeterministic(t *testing.T) {
	s := collector.CollectorStatus{
		ID: "test_v1", Attempted: true, Status: "success",
		RowCount: 10, DurationMS: 5, CollectedAt: "2026-03-16T14:30:01Z",
	}
	d1, _ := json.Marshal(s)
	d2, _ := json.Marshal(s)
	if string(d1) != string(d2) {
		t.Error("JSON must be deterministic")
	}
}

// --- CollectorStatusFile ---

func TestCollectorStatusFileShape(t *testing.T) {
	file := collector.CollectorStatusFile{
		SchemaVersion: "1",
		CollectedAt:   "2026-03-16T14:30:00Z",
		Collectors: []collector.CollectorStatus{
			{ID: "a_v1", Status: "success", Attempted: true},
			{ID: "b_v1", Status: "skipped", Reason: "config_disabled"},
		},
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	_ = json.Unmarshal(data, &m)

	if m["schema_version"] != "1" {
		t.Errorf("schema_version: got %v", m["schema_version"])
	}
	collectors := m["collectors"].([]any)
	if len(collectors) != 2 {
		t.Errorf("collectors: got %d, want 2", len(collectors))
	}
}

func TestCollectorStatusFileEmptyCollectors(t *testing.T) {
	file := collector.CollectorStatusFile{
		SchemaVersion: "1",
		CollectedAt:   "2026-03-16T14:30:00Z",
		Collectors:    []collector.CollectorStatus{},
	}

	data, _ := json.Marshal(file)
	var m map[string]any
	_ = json.Unmarshal(data, &m)

	collectors := m["collectors"].([]any)
	if len(collectors) != 0 {
		t.Error("empty collectors must serialize as []")
	}
}

// --- Status builder helpers ---

func TestBuildSuccessStatus(t *testing.T) {
	now := time.Date(2026, 3, 16, 14, 30, 0, 0, time.UTC)
	s := collector.NewSuccessStatus("pg_stat_user_tables_v1", 47, 12, now)
	if s.Status != "success" || !s.Attempted || s.RowCount != 47 {
		t.Errorf("unexpected: %+v", s)
	}
}

func TestBuildSkippedStatus(t *testing.T) {
	s := collector.NewSkippedStatus("pg_functions_v1", "version_unsupported", "Requires PG 11+")
	if s.Status != "skipped" || s.Attempted || s.Reason != "version_unsupported" {
		t.Errorf("unexpected: %+v", s)
	}
	if s.DurationMS != 0 || s.CollectedAt != "" {
		t.Error("skipped must have zero duration and empty collected_at")
	}
}

func TestBuildFailedStatus(t *testing.T) {
	now := time.Date(2026, 3, 16, 14, 30, 0, 0, time.UTC)
	s := collector.NewFailedStatus("vacuum_health_v1", "permission_denied", "permission denied for pg_stat_all_tables", 3, now)
	if s.Status != "failed" || !s.Attempted || s.Reason != "permission_denied" {
		t.Errorf("unexpected: %+v", s)
	}
}

// --- Ordering ---

func TestCollectorStatusFileOrdering(t *testing.T) {
	file := collector.CollectorStatusFile{
		SchemaVersion: "1",
		Collectors: []collector.CollectorStatus{
			{ID: "z_v1"},
			{ID: "a_v1"},
			{ID: "m_v1"},
		},
	}

	file.Sort()

	ids := make([]string, len(file.Collectors))
	for i, c := range file.Collectors {
		ids[i] = c.ID
	}
	if !sort.StringsAreSorted(ids) {
		t.Errorf("collectors must be sorted by ID: %v", ids)
	}
}
