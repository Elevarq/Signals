package collector

// SingleStore live qualification verifier (specifications/singlestore-live-qualification.md).
//
// These are PURE verification + reader helpers over already-persisted producer
// output: the SQLite store written by the normal cmd/signals daemon and the ZIP
// written by the production exporter. They assert success as status='success'
// with reconciled result evidence (SLQ-R002..R009), never an attempt/row count
// (INV-SLQ-03). The verifier never produces evidence itself — it only checks
// what the real daemon + exporter produced.

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/elevarq/signals/internal/clickhouse"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/singlestore"
)

// QualRun is one persisted query_run projected for qualification, with its
// reconciled persisted-result evidence.
type QualRun struct {
	QueryID       string
	Status        string
	Reason        string
	RowCount      int // collected count captured at mapping time (query_runs.row_count)
	SnapshotID    string
	HasResult     bool // a query_results row exists for this run
	PersistedRows int  // decoded row count of the persisted query_results payload
}

// QualSnapshot is one persisted snapshot projected for qualification.
type QualSnapshot struct {
	ID      string
	Engine  string
	Version string
	Runs    []QualRun
}

// QualExport is the parsed production-export ZIP projected for reconciliation.
type QualExport struct {
	Engine             string
	EngineVersion      string
	CollectorStatusIDs []string
	QueryCatalogIDs    []string
	SnapshotIDs        []string
	ExportedRows       map[string]int // query_id -> exported payload row count (success runs only)
	QueryTextPresent   bool           // MV_PLANCACHE QUERY_TEXT leaked (redaction breach)
	ProcessInfoPresent bool           // MV_PROCESSLIST INFO leaked (redaction breach)
	ManifestNames      map[string]string
	MemberNames        []string
}

// ExpectedCollectorsFor returns the expected collector id set for an engine,
// sorted for deterministic comparison. Wire engines (singlestore/mysql/mariadb)
// come from the MySQL-wire CatalogFor registry; ClickHouse is its own backend
// with its own catalog.
func ExpectedCollectorsFor(engine string) []string {
	var out []string
	if engine == config.EngineClickHouse {
		for _, c := range clickhouse.Catalog() {
			out = append(out, c.ID)
		}
	} else {
		for _, c := range singlestore.CatalogFor(engine) {
			out = append(out, c.ID)
		}
	}
	sort.Strings(out)
	return out
}

// SingleStoreExpectedCollectors returns the expected SingleStore collector id
// set, sorted. Thin wrapper over ExpectedCollectorsFor for the SingleStore
// qualification.
func SingleStoreExpectedCollectors() []string {
	return ExpectedCollectorsFor("singlestore")
}

// VerifyCadenceSnapshot asserts one snapshot's full-grant cadence semantics
// (SLQ-R002..R004, INV-SLQ-03): engine/version, exactly the expected collector
// set, every expected collector status='success', each success reconciled to a
// persisted result whose decoded row count equals row_count (incl. 0==0), and
// every run's snapshot_id matching the enclosing snapshot. Returns a non-nil
// error naming EVERY violation so a partial/all-failed cycle fails loudly.
func VerifyCadenceSnapshot(s QualSnapshot, expected []string, engine, versionPrefix string) error {
	var errs []string
	if s.Engine != engine {
		errs = append(errs, fmt.Sprintf("snapshot %s engine=%q, want %q", s.ID, s.Engine, engine))
	}
	if !strings.HasPrefix(s.Version, versionPrefix) {
		errs = append(errs, fmt.Sprintf("snapshot %s version=%q, want prefix %q", s.ID, s.Version, versionPrefix))
	}

	got := make(map[string]QualRun, len(s.Runs))
	for _, r := range s.Runs {
		got[r.QueryID] = r
		if r.SnapshotID != s.ID {
			errs = append(errs, fmt.Sprintf("run %s snapshot_id=%q != enclosing %q", r.QueryID, r.SnapshotID, s.ID))
		}
	}

	expSet := make(map[string]bool, len(expected))
	for _, id := range expected {
		expSet[id] = true
		r, ok := got[id]
		if !ok {
			errs = append(errs, fmt.Sprintf("expected collector %s missing from snapshot %s", id, s.ID))
			continue
		}
		if r.Status != "success" {
			errs = append(errs, fmt.Sprintf("collector %s status=%q reason=%q, want success", id, r.Status, r.Reason))
			continue
		}
		if !r.HasResult {
			errs = append(errs, fmt.Sprintf("success collector %s has no query_results row", id))
			continue
		}
		if r.PersistedRows != r.RowCount {
			errs = append(errs, fmt.Sprintf("collector %s persisted rows=%d != row_count=%d", id, r.PersistedRows, r.RowCount))
		}
	}
	for id := range got {
		if !expSet[id] {
			errs = append(errs, fmt.Sprintf("unexpected collector %s present in snapshot %s", id, s.ID))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("cadence qualification failed for snapshot %s: %s", s.ID, strings.Join(errs, "; "))
	}
	return nil
}

// VerifyExportReconciliation reconciles one exact snapshot through the exported
// ZIP (SLQ-R006..R009): metadata engine/version, no foreign (PostgreSQL)
// collector in collector_status/query_catalog, collected==persisted==exported
// for every successful collector, failed/skipped never emitted as a successful
// payload, default redaction held, and the manifest covering every member.
func VerifyExportReconciliation(s QualSnapshot, z QualExport, expected []string, versionPrefix string) error {
	var errs []string

	// The exported metadata engine must match the persisted snapshot's engine
	// (engine-generic: singlestore, mysql, …).
	if z.Engine != s.Engine {
		errs = append(errs, fmt.Sprintf("metadata engine=%q, want %q (the snapshot's engine)", z.Engine, s.Engine))
	}
	if z.EngineVersion == "" {
		errs = append(errs, "metadata engine_version is empty")
	} else if !strings.HasPrefix(z.EngineVersion, versionPrefix) {
		errs = append(errs, fmt.Sprintf("metadata engine_version=%q, want prefix %q", z.EngineVersion, versionPrefix))
	}

	expSet := make(map[string]bool, len(expected))
	for _, id := range expected {
		expSet[id] = true
	}
	for _, id := range z.CollectorStatusIDs {
		if !expSet[id] {
			errs = append(errs, fmt.Sprintf("foreign collector %q in collector_status.json", id))
		}
	}
	for _, id := range z.QueryCatalogIDs {
		if !expSet[id] {
			errs = append(errs, fmt.Sprintf("foreign collector %q in query_catalog.json", id))
		}
	}

	for _, r := range s.Runs {
		if r.Status != "success" {
			if _, ok := z.ExportedRows[r.QueryID]; ok {
				errs = append(errs, fmt.Sprintf("collector %s is %q but has an exported payload (must not be emitted as success)", r.QueryID, r.Status))
			}
			continue
		}
		exp, ok := z.ExportedRows[r.QueryID]
		if !ok {
			errs = append(errs, fmt.Sprintf("successful collector %s has no exported payload", r.QueryID))
			continue
		}
		if r.RowCount != r.PersistedRows || r.PersistedRows != exp {
			errs = append(errs, fmt.Sprintf("collector %s rows not reconciled: collected=%d persisted=%d exported=%d", r.QueryID, r.RowCount, r.PersistedRows, exp))
		}
	}

	if z.QueryTextPresent {
		errs = append(errs, "redaction breach: MV_PLANCACHE QUERY_TEXT present in an exported payload")
	}
	if z.ProcessInfoPresent {
		errs = append(errs, "redaction breach: MV_PROCESSLIST INFO present in an exported payload")
	}

	for _, m := range z.MemberNames {
		if m == "manifest.json" {
			continue
		}
		if z.ManifestNames[m] == "" {
			errs = append(errs, fmt.Sprintf("ZIP member %q absent from manifest or has empty sha256", m))
		}
	}
	member := make(map[string]bool, len(z.MemberNames))
	for _, m := range z.MemberNames {
		member[m] = true
	}
	for name := range z.ManifestNames {
		if !member[name] {
			errs = append(errs, fmt.Sprintf("manifest lists member %q absent from the ZIP", name))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("export reconciliation failed for snapshot %s: %s", s.ID, strings.Join(errs, "; "))
	}
	return nil
}

// ReadQualSnapshots projects every persisted snapshot + its runs + reconciled
// result evidence out of a Signals store (the real one the daemon wrote). It
// uses the production db accessors, never a hand-built row.
func ReadQualSnapshots(store *db.DB) ([]QualSnapshot, error) {
	snaps, err := store.GetAllSnapshots("", "")
	if err != nil {
		return nil, fmt.Errorf("get snapshots: %w", err)
	}
	out := make([]QualSnapshot, 0, len(snaps))
	for _, s := range snaps {
		runs, err := store.GetQueryRunsBySnapshotIDs([]string{s.ID})
		if err != nil {
			return nil, fmt.Errorf("runs for snapshot %s: %w", s.ID, err)
		}
		qruns := make([]QualRun, 0, len(runs))
		for _, r := range runs {
			qr := QualRun{
				QueryID: r.QueryID, Status: r.Status, Reason: r.Reason,
				RowCount: r.RowCount, SnapshotID: r.SnapshotID,
			}
			res, err := store.GetQueryResultByRunID(r.ID)
			if err != nil {
				return nil, fmt.Errorf("result for run %s: %w", r.ID, err)
			}
			if res != nil {
				qr.HasResult = true
				decoded, err := db.DecodeNDJSON(res.Payload, res.Compressed)
				if err != nil {
					return nil, fmt.Errorf("decode result for run %s: %w", r.ID, err)
				}
				qr.PersistedRows = len(decoded)
			}
			qruns = append(qruns, qr)
		}
		out = append(out, QualSnapshot{ID: s.ID, Engine: s.Engine, Version: s.PGVersion, Runs: qruns})
	}
	return out, nil
}

// ReadQualExport parses a production-export ZIP into a QualExport for
// reconciliation. It reads the ZIP exactly as a downstream consumer would.
func ReadQualExport(zipPath string) (QualExport, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return QualExport{}, fmt.Errorf("open zip: %w", err)
	}
	defer func() { _ = zr.Close() }()

	z := QualExport{ExportedRows: map[string]int{}, ManifestNames: map[string]string{}}
	runToQuery := map[string]string{}

	read := func(name string) ([]byte, bool, error) {
		for _, f := range zr.File {
			if f.Name == name {
				rc, err := f.Open()
				if err != nil {
					return nil, true, err
				}
				defer rc.Close()
				b, err := io.ReadAll(rc)
				return b, true, err
			}
		}
		return nil, false, nil
	}

	for _, f := range zr.File {
		z.MemberNames = append(z.MemberNames, f.Name)
	}

	// metadata.json
	if b, ok, err := read("metadata.json"); err != nil {
		return z, err
	} else if ok {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return z, fmt.Errorf("metadata.json: %w", err)
		}
		z.Engine, _ = m["engine"].(string)
		z.EngineVersion, _ = m["engine_version"].(string)
	}

	// collector_status.json
	if b, ok, err := read("collector_status.json"); err != nil {
		return z, err
	} else if ok {
		var cs struct {
			Collectors []struct {
				ID string `json:"id"`
			} `json:"collectors"`
		}
		if err := json.Unmarshal(b, &cs); err != nil {
			return z, fmt.Errorf("collector_status.json: %w", err)
		}
		for _, c := range cs.Collectors {
			z.CollectorStatusIDs = append(z.CollectorStatusIDs, c.ID)
		}
	}

	// query_catalog.json (array of {QueryID,...}; db.QueryCatalogRow has no json tags)
	if b, ok, err := read("query_catalog.json"); err != nil {
		return z, err
	} else if ok {
		var rows []struct {
			QueryID string
		}
		if err := json.Unmarshal(b, &rows); err != nil {
			return z, fmt.Errorf("query_catalog.json: %w", err)
		}
		for _, r := range rows {
			z.QueryCatalogIDs = append(z.QueryCatalogIDs, r.QueryID)
		}
	}

	// snapshots.ndjson
	if b, ok, err := read("snapshots.ndjson"); err != nil {
		return z, err
	} else if ok {
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var row struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return z, fmt.Errorf("snapshots.ndjson: %w", err)
			}
			z.SnapshotIDs = append(z.SnapshotIDs, row.ID)
		}
	}

	// query_runs.ndjson -> run_id -> query_id
	if b, ok, err := read("query_runs.ndjson"); err != nil {
		return z, err
	} else if ok {
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var row struct {
				ID      string `json:"id"`
				QueryID string `json:"query_id"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return z, fmt.Errorf("query_runs.ndjson: %w", err)
			}
			runToQuery[row.ID] = row.QueryID
		}
	}

	// query_results.ndjson -> exported payload row count per query_id + redaction scan
	if b, ok, err := read("query_results.ndjson"); err != nil {
		return z, err
	} else if ok {
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var row struct {
				RunID   string           `json:"run_id"`
				Payload []map[string]any `json:"payload"`
			}
			if err := json.Unmarshal([]byte(line), &row); err != nil {
				return z, fmt.Errorf("query_results.ndjson: %w", err)
			}
			if qid, ok := runToQuery[row.RunID]; ok {
				z.ExportedRows[qid] = len(row.Payload)
			}
			for _, r := range row.Payload {
				if v, ok := r["QUERY_TEXT"]; ok && v != nil && v != "" {
					z.QueryTextPresent = true
				}
				if v, ok := r["INFO"]; ok && v != nil && v != "" {
					z.ProcessInfoPresent = true
				}
			}
		}
	}

	// manifest.json
	if b, ok, err := read("manifest.json"); err != nil {
		return z, err
	} else if ok {
		var man struct {
			Files []struct {
				Name   string `json:"name"`
				SHA256 string `json:"sha256"`
			} `json:"files"`
		}
		if err := json.Unmarshal(b, &man); err != nil {
			return z, fmt.Errorf("manifest.json: %w", err)
		}
		for _, f := range man.Files {
			z.ManifestNames[f.Name] = f.SHA256
		}
	}

	return z, nil
}
