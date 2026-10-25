package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/clickhouse"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/pgqueries"
	"github.com/elevarq/signals/internal/singlestore"
)

// Tests derive from specifications/multi-engine-report-contract.acceptance.md.
//
// Deterministic contract-SHAPE conformance over the REAL export.Builder.WriteTo
// path for all five engines. This is serialization/contract coverage, NOT
// producer evidence — producer evidence is each engine's live qualification
// (the *_live tests driven by the dev-image harnesses). Set
// REPORT_CONTRACT_CORPUS_DIR to also write one real export ZIP per engine: the
// #530 conformance corpus the Analyzer owners verify their ingest path against.

func reportContractCatalogIDs(engine string) []string {
	switch engine {
	case config.EnginePostgreSQL:
		ids := make([]string, 0)
		for _, q := range pgqueries.All() {
			ids = append(ids, q.ID)
		}
		return ids
	case config.EngineClickHouse:
		ids := make([]string, 0)
		for _, c := range clickhouse.Catalog() {
			ids = append(ids, c.ID)
		}
		return ids
	default:
		ids := make([]string, 0)
		for _, c := range singlestore.CatalogFor(engine) {
			ids = append(ids, c.ID)
		}
		return ids
	}
}

// exportZIPMembers runs the real export and returns every ZIP member's bytes
// plus the raw ZIP (for the optional corpus writer).
func exportZIPMembers(t *testing.T, store *db.DB, opts Options) (map[string][]byte, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if err := NewBuilder(store, "inst").WriteTo(&buf, opts); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	raw := buf.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	members := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		members[f.Name] = b
	}
	return members, raw
}

func ndjsonLines(b []byte) []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// TestMultiEngineReportContract asserts MERC-R001..R006 + INV-MERC-01/02 across
// all five engines over the real export path (TC-MERC-01..06).
func TestMultiEngineReportContract(t *testing.T) {
	cases := []struct {
		engine  string
		version string
		isPG    bool
	}{
		{config.EnginePostgreSQL, "PostgreSQL 18.0", true},
		{config.EngineSingleStore, "9.1.1", false},
		{config.EngineMySQL, "8.4.13", false},
		{config.EngineMariaDB, "11.4.13", false},
		{config.EngineClickHouse, "24.8", false},
	}
	corpusDir := os.Getenv("REPORT_CONTRACT_CORPUS_DIR")
	members := []string{
		"metadata.json", "collector_status.json", "snapshots.ndjson",
		"query_catalog.json", "query_runs.ndjson", "query_results.ndjson",
		"manifest.json",
	}

	for _, tc := range cases {
		t.Run(tc.engine, func(t *testing.T) {
			catIDs := reportContractCatalogIDs(tc.engine)
			if len(catIDs) == 0 {
				t.Fatalf("engine %s resolved an empty catalog", tc.engine)
			}
			catSet := map[string]bool{}
			for _, id := range catIDs {
				catSet[id] = true
			}

			store := openReportTestDB(t)
			targetID, err := store.UpsertTarget(tc.engine, "h", 3306, "db", "u", "disable", "none", "", true)
			if err != nil {
				t.Fatalf("UpsertTarget: %v", err)
			}
			// seedEngineRun writes snapshot.Engine verbatim. A real PostgreSQL
			// collector persists an EMPTY engine (byte-compat); the export
			// normalizes empty -> "postgresql" (EID-R004). Non-PostgreSQL engines
			// persist their engine explicitly. One representative collector run is
			// enough — collector_status/query_catalog enumerate the full engine
			// catalog regardless.
			seedEngine := tc.engine
			if tc.isPG {
				seedEngine = ""
			}
			seedEngineRun(t, store, tc.engine+"-snap-1", targetID, seedEngine, catIDs[0], "2026-10-23T00:00:00Z", tc.version)

			zipMembers, raw := exportZIPMembers(t, store, Options{TargetID: targetID})

			// MERC-R001: all seven members present.
			for _, m := range members {
				if _, ok := zipMembers[m]; !ok {
					t.Errorf("MERC-R001: member %q missing from %s export", m, tc.engine)
				}
			}

			// metadata.
			var meta map[string]any
			if err := json.Unmarshal(zipMembers["metadata.json"], &meta); err != nil {
				t.Fatalf("decode metadata.json: %v", err)
			}
			// MERC-R002 / INV-MERC-01.
			if meta["schema_version"] != "signals-snapshot.v1" {
				t.Errorf("MERC-R002: schema_version=%v, want signals-snapshot.v1", meta["schema_version"])
			}
			// MERC-R003: engine always present + engine-correct; engine_version
			// non-PostgreSQL-only (PostgreSQL byte-compat gains no new field).
			eng, _ := meta["engine"].(string)
			ev, _ := meta["engine_version"].(string)
			if eng != tc.engine {
				t.Errorf("MERC-R003: engine=%q, want %q", eng, tc.engine)
			}
			if tc.isPG {
				if ev != "" {
					t.Errorf("MERC-R003: PostgreSQL must NOT carry engine_version (byte-compat), got %q", ev)
				}
				if _, present := meta["engine_version"]; present {
					t.Errorf("MERC-R003: PostgreSQL metadata must omit the engine_version key entirely")
				}
			} else if ev == "" {
				t.Errorf("MERC-R003: %s must carry a non-empty engine_version", tc.engine)
			}

			// MERC-R004 / INV-MERC-02: no cross-engine leak in collector_status.
			var cs map[string]any
			if err := json.Unmarshal(zipMembers["collector_status.json"], &cs); err != nil {
				t.Fatalf("decode collector_status.json: %v", err)
			}
			for _, id := range collectorIDs(t, cs) {
				if !catSet[id] {
					t.Errorf("MERC-R004: collector_status leaked %q (not in %s catalog)", id, tc.engine)
				}
			}
			// MERC-R004: no cross-engine leak in query_catalog (array of {QueryID}).
			var qcat []map[string]any
			if err := json.Unmarshal(zipMembers["query_catalog.json"], &qcat); err != nil {
				t.Fatalf("decode query_catalog.json: %v", err)
			}
			for _, r := range qcat {
				id, _ := r["QueryID"].(string)
				if id != "" && !catSet[id] {
					t.Errorf("MERC-R004: query_catalog leaked %q (not in %s catalog)", id, tc.engine)
				}
			}

			// MERC-R005: status enum + success carries a payload.
			runs := ndjsonLines(zipMembers["query_runs.ndjson"])
			if len(runs) == 0 {
				t.Fatalf("MERC-R005: query_runs.ndjson empty for %s", tc.engine)
			}
			successes := 0
			for _, r := range runs {
				st, _ := r["status"].(string)
				switch st {
				case "success":
					successes++
				case "failed", "skipped":
				default:
					t.Errorf("MERC-R005: run %v status=%q outside enum {success,failed,skipped}", r["query_id"], st)
				}
			}
			results := ndjsonLines(zipMembers["query_results.ndjson"])
			if successes > 0 && len(results) == 0 {
				t.Errorf("MERC-R005: %d success runs but query_results.ndjson is empty", successes)
			}

			// MERC-R006: manifest covers every member with a non-empty sha256.
			var man struct {
				Algorithm string `json:"algorithm"`
				Files     []struct {
					Name   string `json:"name"`
					SHA256 string `json:"sha256"`
				} `json:"files"`
			}
			if err := json.Unmarshal(zipMembers["manifest.json"], &man); err != nil {
				t.Fatalf("decode manifest.json: %v", err)
			}
			if man.Algorithm != "sha256" {
				t.Errorf("MERC-R006: manifest algorithm=%q, want sha256", man.Algorithm)
			}
			manifested := map[string]string{}
			for _, f := range man.Files {
				manifested[f.Name] = f.SHA256
				if f.SHA256 == "" {
					t.Errorf("MERC-R006: manifest entry %q has empty sha256", f.Name)
				}
				if _, ok := zipMembers[f.Name]; !ok {
					t.Errorf("MERC-R006: manifest lists %q which is absent from the ZIP", f.Name)
				}
			}
			// Every data member (all but manifest.json itself) must be manifested.
			for _, m := range members {
				if m == "manifest.json" {
					continue
				}
				if manifested[m] == "" {
					t.Errorf("MERC-R006: member %q is not covered by manifest.json", m)
				}
			}

			if corpusDir != "" {
				path := filepath.Join(corpusDir, tc.engine+".zip")
				if err := os.WriteFile(path, raw, 0o644); err != nil {
					t.Fatalf("write corpus %s: %v", path, err)
				}
				t.Logf("#530 corpus: wrote %s (%d bytes)", path, len(raw))
			}
		})
	}
}
