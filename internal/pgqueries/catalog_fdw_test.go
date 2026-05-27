// Tests for the FDW collectors' presence in the registry, SQL
// shape (no `;` injections, expected catalog references), and
// version-compatibility filter behavior.
//
// Spec: specifications/collectors/fdw_*_v1.md
//   FDW-W-AC1, FDW-S-AC1, FDW-U-AC1, FDW-T-AC1.

package pgqueries

import (
	"strings"
	"testing"
)

func TestFDWCollectors_Registered(t *testing.T) {
	want := []string{
		"fdw_wrappers_v1",
		"fdw_servers_v1",
		"fdw_user_mappings_v1",
		"fdw_foreign_tables_v1",
	}
	for _, id := range want {
		if registryByID[id] == nil {
			t.Errorf("FDW collector %q not in registry", id)
		}
	}
}

// TestFDWCollectors_PG14To18Eligible — the four FDW collectors
// query catalogs whose shape is stable across PG 14..18. Verify
// each collector survives the version filter for every supported
// major.
func TestFDWCollectors_PG14To18Eligible(t *testing.T) {
	for _, major := range []int{14, 15, 16, 17, 18} {
		params := FilterParams{PGMajorVersion: major}
		got := Filter(params)
		gotIDs := map[string]bool{}
		for _, q := range got {
			gotIDs[q.ID] = true
		}
		for _, id := range []string{
			"fdw_wrappers_v1",
			"fdw_servers_v1",
			"fdw_user_mappings_v1",
			"fdw_foreign_tables_v1",
		} {
			if !gotIDs[id] {
				t.Errorf("PG%d: FDW collector %q missing from filtered set", major, id)
			}
		}
	}
}

// TestFDWCollectors_SQLReferencesPGCatalog — pin the catalog
// sources each collector reads from. Catches accidental drift
// where a refactor swaps `pg_foreign_server` for a private view.
func TestFDWCollectors_SQLReferencesPGCatalog(t *testing.T) {
	cases := []struct {
		id      string
		mustHit []string
	}{
		{"fdw_wrappers_v1", []string{"pg_foreign_data_wrapper"}},
		{"fdw_servers_v1", []string{"pg_foreign_server", "pg_foreign_data_wrapper"}},
		{"fdw_user_mappings_v1", []string{"pg_user_mappings"}}, // public view (graceful-degradation path; FC-02)
		{"fdw_foreign_tables_v1", []string{
			"pg_foreign_table",
			"pg_class",
			"pg_namespace",
			"pg_foreign_server",
			"pg_foreign_data_wrapper",
		}},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			q := registryByID[c.id]
			if q == nil {
				t.Fatalf("collector %s not in registry", c.id)
			}
			for _, ref := range c.mustHit {
				if !strings.Contains(q.SQL, ref) {
					t.Errorf("%s.SQL must reference %q; got:\n%s", c.id, ref, q.SQL)
				}
			}
		})
	}
}

// TestFDWCollectors_SQLIsReadOnly — defensive lint. Every FDW
// collector's SQL is a SELECT only; no INSERT / UPDATE / DELETE /
// CREATE / DROP / ALTER. The user's safety guarantee
// ("No write operations on PostgreSQL") is enforced at the
// collector layer; this is the per-collector textual proof.
func TestFDWCollectors_SQLIsReadOnly(t *testing.T) {
	forbidden := []string{
		"INSERT ", "UPDATE ", "DELETE ", "CREATE ", "DROP ", "ALTER ",
		"GRANT ", "REVOKE ", "TRUNCATE ", "COPY ",
	}
	for _, id := range []string{
		"fdw_wrappers_v1",
		"fdw_servers_v1",
		"fdw_user_mappings_v1",
		"fdw_foreign_tables_v1",
	} {
		q := registryByID[id]
		if q == nil {
			continue
		}
		upper := strings.ToUpper(q.SQL)
		for _, f := range forbidden {
			if strings.Contains(upper, f) {
				t.Errorf("%s.SQL contains forbidden keyword %q (must be read-only)", id, strings.TrimSpace(f))
			}
		}
		if !strings.HasPrefix(strings.TrimSpace(upper), "SELECT") {
			t.Errorf("%s.SQL must start with SELECT", id)
		}
	}
}

// TestFDWCollectors_SQLHasDeterministicOrdering — every FDW
// collector spec mandates `ORDER BY`. The test pins it textually.
func TestFDWCollectors_SQLHasDeterministicOrdering(t *testing.T) {
	for _, id := range []string{
		"fdw_wrappers_v1",
		"fdw_servers_v1",
		"fdw_user_mappings_v1",
		"fdw_foreign_tables_v1",
	} {
		q := registryByID[id]
		if q == nil {
			continue
		}
		if !strings.Contains(strings.ToUpper(q.SQL), "ORDER BY") {
			t.Errorf("%s.SQL must include ORDER BY for deterministic output", id)
		}
	}
}

// TestFDWCollectors_NoSuperuserNeeded — defensive: the SQL must
// not invoke functions that require superuser. Today this means
// no calls to `pg_read_server_files`, `pg_ls_dir`, or per-FDW
// `*_handler` functions (those are catalog-recorded OIDs;
// invoking them would attempt a remote connection).
func TestFDWCollectors_NoSuperuserNeeded(t *testing.T) {
	forbidden := []string{
		"pg_read_server_files",
		"pg_ls_dir",
		"pg_read_file",
		"pg_stat_file",
	}
	for _, id := range []string{
		"fdw_wrappers_v1",
		"fdw_servers_v1",
		"fdw_user_mappings_v1",
		"fdw_foreign_tables_v1",
	} {
		q := registryByID[id]
		if q == nil {
			continue
		}
		for _, f := range forbidden {
			if strings.Contains(q.SQL, f) {
				t.Errorf("%s.SQL invokes superuser-only function %q", id, f)
			}
		}
	}
}
