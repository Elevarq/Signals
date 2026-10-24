package export

import (
	"testing"

	"github.com/elevarq/signals/internal/pgqueries"
	"github.com/elevarq/signals/internal/singlestore"
)

// Tests derive from specifications/engine-keyed-reporting.acceptance.md.

// TC-MKR-05 (INV-MKR-03): catalogFor is total — postgresql/"" return the
// pgqueries catalog, singlestore returns the SingleStore catalog, and an
// unrecognized engine returns an EMPTY catalog, never a PostgreSQL fallback.
func TestCatalogForIsTotal(t *testing.T) {
	ids := func(cs []reportCollector) map[string]bool {
		m := map[string]bool{}
		for _, c := range cs {
			m[c.ID] = true
		}
		return m
	}

	pg := ids(catalogFor("postgresql"))
	if len(pg) != len(pgqueries.All()) {
		t.Errorf("catalogFor(postgresql) has %d entries, want %d", len(pg), len(pgqueries.All()))
	}
	for _, q := range pgqueries.All() {
		if !pg[q.ID] {
			t.Errorf("catalogFor(postgresql) missing %q", q.ID)
		}
	}

	empty := ids(catalogFor(""))
	if len(empty) != len(pg) {
		t.Errorf("catalogFor(\"\") must equal catalogFor(postgresql): %d vs %d", len(empty), len(pg))
	}

	ss := ids(catalogFor("singlestore"))
	if len(ss) != len(singlestore.Catalog()) {
		t.Errorf("catalogFor(singlestore) has %d entries, want %d", len(ss), len(singlestore.Catalog()))
	}
	for _, c := range singlestore.Catalog() {
		if !ss[c.ID] {
			t.Errorf("catalogFor(singlestore) missing %q", c.ID)
		}
	}
	// No PostgreSQL id leaks into the SingleStore catalog.
	for _, q := range pgqueries.All() {
		if ss[q.ID] {
			t.Errorf("catalogFor(singlestore) leaked PostgreSQL id %q", q.ID)
		}
	}

	// mysql is now a supported wire engine: its reporting catalog comes from the
	// single registry (no PostgreSQL fallback, no PG ids).
	my := ids(catalogFor("mysql"))
	if len(my) != len(singlestore.CatalogFor("mysql")) || !my["mysql_identity_v1"] {
		t.Errorf("catalogFor(mysql) wrong: %v", my)
	}
	for _, q := range pgqueries.All() {
		if my[q.ID] {
			t.Errorf("catalogFor(mysql) leaked PostgreSQL id %q", q.ID)
		}
	}
	if got := catalogFor("nonsense-engine"); len(got) != 0 {
		t.Errorf("catalogFor(unknown) = %d entries, want 0 (no fallback)", len(got))
	}
}
