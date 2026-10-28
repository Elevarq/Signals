// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

package export

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/elevarq/signals/internal/db"
)

// #531 (MKR-R008) — the server version is stored in the legacy PGVersion
// column for every engine, but it must be serialized truthfully per engine in
// the export rows: a non-PostgreSQL engine must NEVER emit its version as
// `pg_version` (the 2026-10-23 forensic defect: a real SingleStore export
// wrote engine_version 9.1.1 into pg_version in snapshots.ndjson and
// query_runs.ndjson). PostgreSQL keeps `pg_version` unchanged (INV-MKR-02).

func exportNDJSONRows(t *testing.T, store *db.DB, targetID int64, member string) []map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := NewBuilder(store, "inst").WriteTo(&buf, Options{TargetID: targetID}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name != member {
			continue
		}
		rc, _ := f.Open()
		var raw bytes.Buffer
		_, _ = raw.ReadFrom(rc)
		rc.Close()
		var rows []map[string]any
		for _, line := range strings.Split(strings.TrimSpace(raw.String()), "\n") {
			if line == "" {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("parse %s line %q: %v", member, line, err)
			}
			rows = append(rows, m)
		}
		return rows
	}
	t.Fatalf("%s not present in export", member)
	return nil
}

func seedCollection(t *testing.T, store *db.DB, targetID int64, snapID, engine, version string) {
	t.Helper()
	snap := db.Snapshot{
		ID: snapID, TargetID: targetID, CollectedAt: "2026-10-23T00:00:00Z",
		PGVersion: version, Engine: engine,
		Payload: json.RawMessage(`{}`), SizeBytes: 2,
	}
	run := db.QueryRun{
		ID: "run-" + snapID, TargetID: targetID, SnapshotID: snapID,
		QueryID: "q1", CollectedAt: "2026-10-23T00:00:00Z", PGVersion: version,
		CreatedAt: "2026-10-23T00:00:00Z", Status: "success", RowCount: 1,
	}
	results := []db.QueryResult{{RunID: run.ID, Payload: []byte("{\"k\":\"v\"}\n"), SizeBytes: 8}}
	if err := store.InsertCollectionAtomic(snap, []db.QueryRun{run}, results); err != nil {
		t.Fatalf("InsertCollectionAtomic: %v", err)
	}
}

func newStore(t *testing.T) *db.DB {
	t.Helper()
	store, err := db.Open(filepath.Join(t.TempDir(), "x.db"), false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return store
}

// A SingleStore export carries truthful engine identity and NEVER serializes
// its version as pg_version — in both snapshots.ndjson and query_runs.ndjson.
func TestSingleStoreExportRowsVersionTruthful(t *testing.T) {
	store := newStore(t)
	targetID, err := store.UpsertTarget("ss", "h", 3306, "memsql", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	seedCollection(t, store, targetID, "snap-ss", "singlestore", "9.1.1")

	for _, member := range []string{"snapshots.ndjson", "query_runs.ndjson"} {
		rows := exportNDJSONRows(t, store, targetID, member)
		if len(rows) == 0 {
			t.Fatalf("%s: no rows", member)
		}
		for _, r := range rows {
			if _, has := r["pg_version"]; has {
				t.Errorf("%s: non-PG row must not carry pg_version (got %v)", member, r["pg_version"])
			}
			if r["engine"] != "singlestore" {
				t.Errorf("%s: engine = %v, want singlestore", member, r["engine"])
			}
			if r["engine_version"] != "9.1.1" {
				t.Errorf("%s: engine_version = %v, want 9.1.1", member, r["engine_version"])
			}
		}
	}
}

// A PostgreSQL export is unchanged: pg_version carries the version; no
// engine/engine_version fields are added to the rows (byte-identical, INV-MKR-02).
func TestPostgreSQLExportRowsUnchanged(t *testing.T) {
	store := newStore(t)
	targetID, err := store.UpsertTarget("pg", "h", 5432, "postgres", "u", "disable", "none", "", true)
	if err != nil {
		t.Fatalf("UpsertTarget: %v", err)
	}
	seedCollection(t, store, targetID, "snap-pg", "postgresql", "16.2")

	for _, member := range []string{"snapshots.ndjson", "query_runs.ndjson"} {
		rows := exportNDJSONRows(t, store, targetID, member)
		if len(rows) == 0 {
			t.Fatalf("%s: no rows", member)
		}
		for _, r := range rows {
			if r["pg_version"] != "16.2" {
				t.Errorf("%s: pg_version = %v, want 16.2", member, r["pg_version"])
			}
			if _, has := r["engine_version"]; has {
				t.Errorf("%s: PG row must not add engine_version (byte-identical)", member)
			}
			if _, has := r["engine"]; has {
				t.Errorf("%s: PG row must not add engine field (byte-identical)", member)
			}
		}
	}
}
