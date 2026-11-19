package pgqueries

import "testing"

// Tests for #816 — separate the actual server major (capability gating)
// from the compatible SQL baseline (override resolution). See
// specifications/catalog-version-selection.md (CVS-R001..R005).

// CVS-AT-01 — catalogBaseline() falls back to the actual major when the
// baseline field is unset (the pre-#816 caller shape), and uses the
// clamped baseline when set.
func TestCatalogBaseline_FallsBackToActual(t *testing.T) {
	if got := (FilterParams{PGMajorVersion: 17}).catalogBaseline(); got != 17 {
		t.Errorf("catalogBaseline() with no CatalogBaselineMajor = %d, want 17 (actual major)", got)
	}
	if got := (FilterParams{PGMajorVersion: 19, CatalogBaselineMajor: 18}).catalogBaseline(); got != 18 {
		t.Errorf("catalogBaseline() with CatalogBaselineMajor=18 = %d, want 18 (clamped baseline)", got)
	}
	// A zero actual major with no baseline stays 0 (no implicit default).
	if got := (FilterParams{}).catalogBaseline(); got != 0 {
		t.Errorf("catalogBaseline() zero value = %d, want 0", got)
	}
}

// CVS-AT-02 — the MinPGVersion capability gate keys on the ACTUAL server
// major. A MinPGVersion:19 collector is blocked on PG 14-18 and admitted
// on PG 19 and PG 20; a MinPGVersion:0 collector is never blocked.
func TestVersionGateBlocks_KeysOnActualMajor(t *testing.T) {
	pg19Only := QueryDef{ID: "synthetic_pg19_only", MinPGVersion: 19}
	for _, actual := range []int{14, 15, 16, 17, 18} {
		if !versionGateBlocks(pg19Only, actual) {
			t.Errorf("versionGateBlocks(MinPGVersion:19, actual=%d) = false, want true (gated below floor)", actual)
		}
	}
	for _, actual := range []int{19, 20} {
		if versionGateBlocks(pg19Only, actual) {
			t.Errorf("versionGateBlocks(MinPGVersion:19, actual=%d) = true, want false (server meets the floor)", actual)
		}
	}

	noFloor := QueryDef{ID: "synthetic_no_floor"}
	for _, actual := range []int{14, 18, 19, 20} {
		if versionGateBlocks(noFloor, actual) {
			t.Errorf("versionGateBlocks(MinPGVersion:0, actual=%d) = true, want false (no floor)", actual)
		}
	}
}

// findSQL returns the resolved SQL of a logical collector in a Filter
// result, or "" when it is not eligible.
func findSQL(qs []QueryDef, id string) string {
	for _, q := range qs {
		if q.ID == id {
			return q.SQL
		}
	}
	return ""
}

// CVS-AT-03 — SQL override resolution keys on the BASELINE, not the
// actual major. pg_stat_io_v1 (MinPGVersion:16) has a PG18 override and
// no PG19 override (catalog_pg19.go is a placeholder). On a real PG19
// target (actual=19, baseline=18) the collector must be eligible AND
// resolve to the PG18 override SQL — proving the actual major gates and
// the baseline resolves. If resolution wrongly used the actual major 19,
// it would fall through to the default (non-18) SQL.
func TestFilter_ResolvesSQLAgainstBaselineNotActual(t *testing.T) {
	const id = "pg_stat_io_v1"

	sqlAt18 := findSQL(Filter(FilterParams{PGMajorVersion: 18}), id) // the PG18 override
	sqlAt17 := findSQL(Filter(FilterParams{PGMajorVersion: 17}), id) // default (no PG17 override)
	if sqlAt18 == "" {
		t.Fatalf("%s not eligible at PG18 — test premise broken", id)
	}
	if sqlAt18 == sqlAt17 {
		t.Fatalf("%s PG18 override equals PG17 default — cannot distinguish baseline resolution", id)
	}

	got := findSQL(Filter(FilterParams{PGMajorVersion: 19, CatalogBaselineMajor: 18}), id)
	if got == "" {
		t.Fatalf("%s gated on a real PG19 target (actual=19, baseline=18) — capability gate used the baseline, not the actual major (#816 regression)", id)
	}
	if got != sqlAt18 {
		t.Errorf("%s on (actual=19, baseline=18) resolved SQL does not match the PG18 override:\n got:  %q\n want: %q\n(resolution must key on the baseline, not the actual major)", id, got, sqlAt18)
	}
}

// CVS-AT-04 — for every in-window major (PG14-18), passing
// CatalogBaselineMajor=0 and CatalogBaselineMajor==PGMajorVersion yield
// byte-identical Filter results. The new field cannot perturb the
// supported window.
func TestFilter_InWindowUnchangedByBaselineField(t *testing.T) {
	for _, major := range []int{14, 15, 16, 17, 18} {
		base := Filter(FilterParams{PGMajorVersion: major})
		withField := Filter(FilterParams{PGMajorVersion: major, CatalogBaselineMajor: major})
		if len(base) != len(withField) {
			t.Fatalf("PG%d: eligibility count changed with CatalogBaselineMajor set: %d vs %d", major, len(base), len(withField))
		}
		for i := range base {
			if base[i].ID != withField[i].ID || base[i].SQL != withField[i].SQL {
				t.Errorf("PG%d: collector %q diverged when CatalogBaselineMajor==PGMajorVersion", major, base[i].ID)
			}
		}
	}
}
