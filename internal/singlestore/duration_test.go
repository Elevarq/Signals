package singlestore

import (
	"context"
	"testing"
	"time"
)

// Tests derive from specifications/engine-keyed-reporting.acceptance.md.

// TC-MKR-08 (MKR-R006): Collect measures each collector's wall-clock duration.
// An injected step clock advances a fixed delta across each collector's
// statement, so every successful result's DurationMS equals that delta — a
// deterministic proof the duration is MEASURED, never a hardcoded 0.
func TestCollectMeasuresDuration(t *testing.T) {
	const step = 10 * time.Millisecond
	base := time.Unix(1_700_000_000, 0).UTC()
	var calls int
	clock := func() time.Time {
		t := base.Add(time.Duration(calls) * step)
		calls++
		return t
	}

	rs := CollectWithClock(context.Background(), stubRunner(""), Catalog(), false, false, clock)
	if len(rs) != len(Catalog()) {
		t.Fatalf("want a result per collector (%d), got %d", len(Catalog()), len(rs))
	}
	for _, r := range rs {
		if r.Err != "" {
			continue
		}
		if r.DurationMS != int(step.Milliseconds()) {
			t.Errorf("collector %q DurationMS = %d, want %d (measured, not 0)", r.CollectorID, r.DurationMS, step.Milliseconds())
		}
	}
}

// TC-MKR-08 companion: the production Collect wrapper populates a measured,
// non-negative duration (real wall clock) for each result.
func TestCollectPopulatesDuration(t *testing.T) {
	rs := Collect(context.Background(), stubRunner(""), Catalog(), false, false)
	for _, r := range rs {
		if r.DurationMS < 0 {
			t.Errorf("collector %q DurationMS = %d, want >= 0", r.CollectorID, r.DurationMS)
		}
	}
}

// TC-MKR-08 companion: the catalog's ElevatedPrivilege is carried onto each
// Result so the persistence layer can classify an elevated permission-denied
// as skipped (MKR-R007).
func TestCollectCarriesElevatedPrivilege(t *testing.T) {
	rs := Collect(context.Background(), stubRunner(""), Catalog(), false, false)
	got := resultByID(rs, "singlestore_cluster_status_v1")
	if got == nil {
		t.Fatal("cluster_status result missing")
	}
	if got.ElevatedPrivilege != "CLUSTER" {
		t.Errorf("cluster_status ElevatedPrivilege = %q, want CLUSTER", got.ElevatedPrivilege)
	}
}

// IsPermissionDenied recognizes a MySQL-wire permission error (MKR-R007).
func TestIsPermissionDenied(t *testing.T) {
	cases := []struct {
		msg  string
		want bool
	}{
		{"Error 1142 (42000): SELECT command denied to user 'mon'@'%' for table 'MV_CLUSTER_STATUS'", true},
		{"Error 1227 (42000): Access denied; you need (at least one of) the CLUSTER privilege(s)", true},
		{"Error 1045 (28000): Access denied for user 'mon'@'%'", true},
		{"Error 1064 (42000): You have an error in your SQL syntax", false},
		{"dial tcp: connection refused", false},
		{"", false},
	}
	for _, c := range cases {
		if got := IsPermissionDenied(c.msg); got != c.want {
			t.Errorf("IsPermissionDenied(%q) = %v, want %v", c.msg, got, c.want)
		}
	}
}
