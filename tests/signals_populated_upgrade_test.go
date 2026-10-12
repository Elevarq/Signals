// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

// Populated vPREV -> vNEXT upgrade regression gate (Elevarq/Signals#504).
//
// This is NOT a fresh-install test and NOT a same-version restart. It proves
// the #501 persistence guarantees and the #502 forward-only schema guarantees
// hold across a REAL upgrade: a store last written by an EARLIER schema
// version (vPREV — a strict prefix of the embedded migration set), populated
// with real instance identity + collection history + cadence watermarks +
// cycle outcomes + by-reference config, is opened and migrated FORWARD by the
// current binary (vNEXT), and every piece of state survives while the next
// collection CONTINUES from the watermarks instead of restarting from zero.
//
// The upgrade is performed through the real mechanism (db.Open -> Migrate),
// never by hand-executing migration SQL in the assertion path. State
// semantics are deployment-independent (they live in internal/db), so this
// single deep test is the primary proof for every deploy mode; the per-mode
// mapping is structural (see TestPopulatedUpgrade_ContainerDataPathDurable
// here + the #501 guards in tests/data_durability_test.go).
//
// Spec: specifications/populated-upgrade-gate.md
// Acceptance: specifications/populated-upgrade-gate.acceptance.md
// Invariants under test (reused): #501 INV-DUR-02/04, #502 INV-SCHEMA-03/04.
// Traces: R-UPG-01..R-UPG-08, INV-UPG-01..04, B1..B8.
package tests

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/db"
)

// readMigrationSQL reads an embedded migration's SQL from the on-disk source
// under internal/db/migrations (the same files MigrationFilenames() embeds),
// so the tests package can replay a strict prefix via ApplyMigrationSQL
// without needing a test-only production API.
func readMigrationSQL(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join("..", "internal", "db", "migrations", name)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read migration %s: %v", p, err)
	}
	return string(b)
}

// readLedger returns the schema_migrations filenames recorded in the store at
// dbPath, sorted. It opens its own read-only-style connection so it can be
// called before and after the upgrade without holding the test's handle.
func readLedger(t *testing.T, dbPath string) []string {
	t.Helper()
	conn, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open ledger conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	rows, err := conn.Query("SELECT filename FROM schema_migrations ORDER BY filename")
	if err != nil {
		t.Fatalf("query ledger: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			t.Fatalf("scan ledger: %v", err)
		}
		out = append(out, f)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate ledger: %v", err)
	}
	sort.Strings(out)
	return out
}

// readLedgerAppliedAt returns filename -> applied_at for every ledger row.
func readLedgerAppliedAt(t *testing.T, dbPath string) map[string]string {
	t.Helper()
	conn, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		t.Fatalf("open ledger conn: %v", err)
	}
	defer func() { _ = conn.Close() }()
	rows, err := conn.Query("SELECT filename, applied_at FROM schema_migrations")
	if err != nil {
		t.Fatalf("query ledger applied_at: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var f, a string
		if err := rows.Scan(&f, &a); err != nil {
			t.Fatalf("scan ledger applied_at: %v", err)
		}
		out[f] = a
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate ledger applied_at: %v", err)
	}
	return out
}

// equalStringSlice reports whether two string slices are element-wise equal.
func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// buildVPrevStore constructs a populated store at an EARLIER embedded
// migration state: it applies only a strict prefix of MigrationFilenames()
// (all but the last migration) via the real atomic migration path, then
// populates it with real history/identity/watermarks/config using the
// internal/db API. It returns the applied prefix and the dropped suffix so
// the caller can assert the ledger advance (B5). Modelling vPREV as
// "embedded set minus the final migration" guarantees >=1 missing migration
// (a genuine upgrade) and keeps the query_runs status/reason columns
// (migration 004) and cycle-outcomes table (005) available for a rich
// fixture, while the upgrade applies the final migration — the circuit_state
// table (006) — so a suffix-migration capability (UpsertCircuitState) can be
// exercised post-upgrade (B6). (R-UPG-01 / R-UPG-02)
func buildVPrevStore(t *testing.T, dbPath string, fx *upgradeFixture) (prefix, suffix []string) {
	t.Helper()

	embedded := append([]string(nil), db.MigrationFilenames()...)
	sort.Strings(embedded)
	if len(embedded) < 2 {
		t.Skipf("need >=2 embedded migrations to model a vPREV->vNEXT upgrade, have %d", len(embedded))
	}
	prefix = embedded[:len(embedded)-1]
	suffix = embedded[len(embedded)-1:]

	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("vPREV db.Open: %v", err)
	}
	defer func() { _ = store.Close() }()

	// Create the ledger table exactly as Migrate would, then apply ONLY the
	// prefix migrations through the real atomic path — the authentic shape of
	// an earlier release's store (R-UPG-01).
	if _, err := store.SQL().Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatalf("vPREV create ledger: %v", err)
	}
	for _, name := range prefix {
		data := readMigrationSQL(t, name)
		if err := store.ApplyMigrationSQL(name, data); err != nil {
			t.Fatalf("vPREV apply prefix migration %s: %v", name, err)
		}
	}

	// --- Populate the vPREV store with representative state (R-UPG-02). ---

	// Instance identity.
	fx.instanceID, err = store.EnsureInstanceID()
	if err != nil {
		t.Fatalf("vPREV EnsureInstanceID: %v", err)
	}
	if fx.instanceID == "" {
		t.Fatal("vPREV produced an empty instance_id")
	}

	// A target carrying a by-REFERENCE credential — secret_type + secret_ref,
	// NEVER a secret value (INV-UPG-03 / INV-SIGNALS-07).
	fx.targetID, err = store.UpsertTarget(
		fx.targetName, "db.internal.example.com", 5432, "appdb", "signals",
		"verify-full", fx.secretType, fx.secretRef, true)
	if err != nil {
		t.Fatalf("vPREV UpsertTarget: %v", err)
	}

	// Two cycles of collection history, the second establishing the
	// watermark. query_results payload on the watermark run so B3 can assert
	// the payload survived.
	for i, stamp := range []string{fx.olderStamp, fx.watermarkStamp} {
		snapID := fmt.Sprintf("snap-%d", i)
		snap := db.Snapshot{ID: snapID, TargetID: fx.targetID, CollectedAt: stamp, PGVersion: "17.4", Payload: []byte(`{"ok":true}`), SizeBytes: 11}
		run := db.QueryRun{
			ID: fmt.Sprintf("run-%d", i), TargetID: fx.targetID, SnapshotID: snapID,
			QueryID: fx.watermarkQueryID, CollectedAt: stamp, PGVersion: "17.4",
			CreatedAt: stamp, Status: "success",
		}
		var results []db.QueryResult
		if i == 1 {
			results = []db.QueryResult{{RunID: run.ID, Payload: fx.resultPayload, Compressed: false, SizeBytes: len(fx.resultPayload)}}
		}
		if err := store.InsertCollectionAtomic(snap, []db.QueryRun{run}, results); err != nil {
			t.Fatalf("vPREV InsertCollectionAtomic[%d]: %v", i, err)
		}
		fx.wantSnapshots++
		fx.wantRuns++
	}

	// A per-target cycle outcome (migration 005 is in the prefix).
	if err := store.RecordCycleOutcome(fx.targetName, "success", ""); err != nil {
		t.Fatalf("vPREV RecordCycleOutcome: %v", err)
	}

	return prefix, suffix
}

// upgradeFixture carries the pre-upgrade state so the post-upgrade assertions
// can compare against the exact values written at vPREV.
type upgradeFixture struct {
	targetName       string
	secretType       string
	secretRef        string
	watermarkQueryID string
	olderStamp       string
	watermarkStamp   string
	resultPayload    []byte

	instanceID    string
	targetID      int64
	wantSnapshots int
	wantRuns      int
}

// TestPopulatedUpgrade_StateSurvivesVPrevToVNext is the deep lifecycle proof:
// populate a vPREV store, upgrade it forward with the current binary through
// the real Open->Migrate mechanism, and assert identity/history/watermark
// continuity, correct ledger advance, post-upgrade usability (including a
// suffix-migration capability), no credential in the store, and no empty
// re-init over populated data.
//
// Covers TC-UPG-01..07 (B1..B7).
func TestPopulatedUpgrade_StateSurvivesVPrevToVNext(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "signals.db") // the durable /data volume

	fx := &upgradeFixture{
		targetName:       "pg-prod",
		secretType:       "aws-rds-iam",
		secretRef:        "arn:aws:rds-db:us-east-1:123456789012:dbuser:db-ABCDE/signals",
		watermarkQueryID: "pg_stat_statements_v1",
		olderStamp:       "2026-10-12T09:55:00Z",
		watermarkStamp:   "2026-10-12T10:00:00Z",
		resultPayload:    []byte(`{"rows":[{"queryid":1,"calls":42}]}`),
	}

	// --- vPREV: build + populate the earlier-schema store. ---
	prefix, suffix := buildVPrevStore(t, dbPath, fx)

	embedded := append([]string(nil), db.MigrationFilenames()...)
	sort.Strings(embedded)

	// B1 / TC-UPG-01: before upgrade the ledger is a genuine strict prefix.
	preLedger := readLedger(t, dbPath)
	if !equalStringSlice(preLedger, prefix) {
		t.Fatalf("vPREV ledger = %v, want strict prefix %v", preLedger, prefix)
	}
	if len(preLedger) >= len(embedded) {
		t.Fatalf("vPREV ledger is not a STRICT prefix (len %d >= embedded %d) — not an upgrade", len(preLedger), len(embedded))
	}
	known := map[string]struct{}{}
	for _, m := range embedded {
		known[m] = struct{}{}
	}
	for _, f := range preLedger {
		if _, ok := known[f]; !ok {
			t.Fatalf("vPREV ledger has unknown filename %q — must not be a NEWER store (#502 B5)", f)
		}
	}

	// Capture the prefix rows' applied_at so B5 can assert they are NOT
	// rewritten by the upgrade.
	preApplied := readLedgerAppliedAt(t, dbPath)

	// --- THE SUPPORTED UPGRADE: open with the current binary + Migrate forward
	// (R-UPG-03). This is the real mechanism, not hand-run SQL. ---
	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("vNEXT db.Open (open vPREV store with current binary): %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatalf("vNEXT Migrate (forward upgrade of populated vPREV store): %v", err)
	}

	// B5 / TC-UPG-05: ledger advanced to the full embedded set, forward-only.
	postLedger := readLedger(t, dbPath)
	if !equalStringSlice(postLedger, embedded) {
		t.Fatalf("post-upgrade ledger = %v, want full embedded set %v", postLedger, embedded)
	}
	// The dropped suffix migration(s) are now recorded.
	postSet := map[string]struct{}{}
	for _, f := range postLedger {
		postSet[f] = struct{}{}
	}
	for _, s := range suffix {
		if _, ok := postSet[s]; !ok {
			t.Errorf("suffix migration %q not recorded after upgrade — ledger did not advance (B5)", s)
		}
	}
	// Prefix rows unchanged (applied_at not rewritten) — forward-only advance
	// by exactly the missing migrations (INV-UPG-02 / INV-SCHEMA-04).
	postApplied := readLedgerAppliedAt(t, dbPath)
	for _, p := range prefix {
		if preApplied[p] != postApplied[p] {
			t.Errorf("prefix migration %q applied_at rewritten by upgrade: %q -> %q (INV-UPG-02)", p, preApplied[p], postApplied[p])
		}
	}

	// B2 / TC-UPG-02: identity unchanged.
	gotID, err := store.EnsureInstanceID()
	if err != nil {
		t.Fatalf("post-upgrade EnsureInstanceID: %v", err)
	}
	if gotID != fx.instanceID {
		t.Errorf("instance_id changed across upgrade: got %q, want %q (B2 / INV-UPG-01)", gotID, fx.instanceID)
	}

	// B3 / TC-UPG-03: collection history preserved (counts + payload identity).
	gotSnaps, err := store.CountSnapshots()
	if err != nil {
		t.Fatalf("post-upgrade CountSnapshots: %v", err)
	}
	if gotSnaps != fx.wantSnapshots {
		t.Errorf("snapshot history lost across upgrade: got %d, want %d (B3 / INV-UPG-01)", gotSnaps, fx.wantSnapshots)
	}
	gotRuns, err := store.GetQueryRunsByTarget(fx.targetID, "", "")
	if err != nil {
		t.Fatalf("post-upgrade GetQueryRunsByTarget: %v", err)
	}
	if len(gotRuns) != fx.wantRuns {
		t.Errorf("query-run history lost across upgrade: got %d, want %d (B3 / INV-UPG-01)", len(gotRuns), fx.wantRuns)
	}
	res, err := store.GetQueryResultByRunID("run-1")
	if err != nil {
		t.Fatalf("post-upgrade GetQueryResultByRunID: %v", err)
	}
	if res == nil {
		t.Fatalf("query_result payload lost across upgrade (B3 / INV-UPG-01)")
	}
	if !bytes.Equal(res.Payload, fx.resultPayload) {
		t.Errorf("query_result payload corrupted across upgrade: got %q, want %q (B3)", res.Payload, fx.resultPayload)
	}

	// B4 / TC-UPG-04: watermark preserved AND the next collection continues.
	wm, err := store.GetLastRunTimes(fx.targetID)
	if err != nil {
		t.Fatalf("post-upgrade GetLastRunTimes: %v", err)
	}
	got, ok := wm[fx.watermarkQueryID]
	if !ok {
		t.Fatalf("watermark for %q lost across upgrade — next cycle would re-collect from zero (B4 / INV-UPG-01)", fx.watermarkQueryID)
	}
	wantWM, _ := time.Parse(time.RFC3339, fx.watermarkStamp)
	if !got.Equal(wantWM) {
		t.Errorf("watermark drifted across upgrade: got %s, want %s (B4)", got, wantWM)
	}

	// B6 / TC-UPG-06: a subsequent collection succeeds and CONTINUES from the
	// watermark (strictly after it), and a suffix-migration capability
	// (circuit_state, added by the applied suffix migration) is usable.
	nextStamp := "2026-10-12T10:05:00Z"
	nextWM, _ := time.Parse(time.RFC3339, nextStamp)
	if !nextWM.After(wantWM) {
		t.Fatalf("test setup: next stamp %s not after watermark %s", nextStamp, wantWM)
	}
	nextSnap := db.Snapshot{ID: "snap-next", TargetID: fx.targetID, CollectedAt: nextStamp, PGVersion: "17.4", Payload: []byte(`{"ok":true}`), SizeBytes: 11}
	nextRun := db.QueryRun{
		ID: "run-next", TargetID: fx.targetID, SnapshotID: "snap-next",
		QueryID: fx.watermarkQueryID, CollectedAt: nextStamp, PGVersion: "17.4",
		CreatedAt: nextStamp, Status: "success",
	}
	if err := store.InsertCollectionAtomic(nextSnap, []db.QueryRun{nextRun}, nil); err != nil {
		t.Fatalf("post-upgrade InsertCollectionAtomic (next cycle): %v (B6)", err)
	}
	wm2, err := store.GetLastRunTimes(fx.targetID)
	if err != nil {
		t.Fatalf("post-next-cycle GetLastRunTimes: %v", err)
	}
	if adv := wm2[fx.watermarkQueryID]; !adv.Equal(nextWM) {
		t.Errorf("next collection did not advance the watermark incrementally: got %s, want %s (B4/B6 — continue-not-restart)", adv, nextWM)
	}
	// Suffix-migration capability: circuit_state (migration 006) exists and is
	// writable after the upgrade.
	if err := store.UpsertCircuitState(fx.targetName, "open", nextStamp, "too_many_failures", "auto", nextStamp); err != nil {
		t.Fatalf("post-upgrade UpsertCircuitState (suffix-migration capability): %v (B6)", err)
	}
	states, err := store.GetCircuitStates()
	if err != nil {
		t.Fatalf("post-upgrade GetCircuitStates: %v", err)
	}
	if len(states) != 1 || states[0].TargetName != fx.targetName {
		t.Errorf("circuit_state not usable after upgrade: %+v (B6)", states)
	}

	// B7 / TC-UPG-07 (a): no credential value in the store.
	for _, k := range []string{"password", "api_token", "token", "secret", "db_password"} {
		v, err := store.GetMeta(k)
		if err != nil {
			t.Fatalf("post-upgrade GetMeta(%q): %v", k, err)
		}
		if v != "" {
			t.Errorf("credential-like meta key %q present after upgrade (B7 / INV-UPG-03)", k)
		}
	}
	// The target persists only the by-reference secret identifier — no
	// password/token value leaked into the ref.
	targets, err := store.GetTargets()
	if err != nil {
		t.Fatalf("post-upgrade GetTargets: %v", err)
	}
	var found bool
	for _, tg := range targets {
		if tg.Name != fx.targetName {
			continue
		}
		found = true
		if tg.SecretType != fx.secretType || tg.SecretRef != fx.secretRef {
			t.Errorf("target secret reference changed across upgrade: got (%q,%q), want (%q,%q) (B7)",
				tg.SecretType, tg.SecretRef, fx.secretType, fx.secretRef)
		}
		if containsCredentialWord(tg.SecretRef) {
			t.Errorf("target secret_ref %q looks like a credential value, not a reference (B7 / INV-UPG-03)", tg.SecretRef)
		}
	}
	if !found {
		t.Fatalf("target %q missing after upgrade (B7)", fx.targetName)
	}

	// B7 / TC-UPG-07 (b): the upgrade did NOT initialize an empty store over
	// the populated one. Identity + history + watermark are the pre-upgrade
	// values, not defaults. (The positive assertions above already prove the
	// values survived; this is the explicit no-empty-init check: the counts
	// are the pre-upgrade counts, not zero.)
	if gotSnaps == 0 || len(gotRuns) == 0 {
		t.Fatalf("upgrade initialized an empty store over populated data (snaps=%d runs=%d) (B7 / INV-UPG-02)", gotSnaps, len(gotRuns))
	}
}

// containsCredentialWord reports whether s contains a substring that would
// indicate a credential VALUE rather than a reference. A reference (ARN,
// secret name, URI) is fine; a literal "password=" / "token=" is not.
func containsCredentialWord(s string) bool {
	for _, w := range []string{"password=", "token=", "secret=", "BEGIN PRIVATE KEY"} {
		if bytes.Contains([]byte(s), []byte(w)) {
			return true
		}
	}
	return false
}

// TestPopulatedUpgrade_ContainerDataPathDurable is the structural mapping of
// the deployment-independent upgrade proof (TC-UPG-02..07) to the container /
// Compose deploy mode (TC-UPG-08 / B8 / R-UPG-05). Because the state proof is
// deployment-independent, each supported mode needs only a durable /data for
// that proof to apply across an instance/pod/AMI replacement — which is the
// upgrade. The #501 guards (tests/data_durability_test.go) already assert the
// Helm PVC durable default and the cloud VM/AMI durable-volume retain; this
// guard adds the container/Compose mapping: the production Compose example
// must back /data with a DECLARED named volume (survives container recreate),
// not an anonymous/ephemeral mount. It does NOT re-run the deep lifecycle
// proof — it maps the single proof to this mode (R-UPG-05).
func TestPopulatedUpgrade_ContainerDataPathDurable(t *testing.T) {
	path := filepath.Join("..", "examples", "docker-compose.prod.yml")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	src := string(b)

	// The service mounts /data from a NAMED volume (named:path form), not an
	// anonymous volume (bare "/data") or a throwaway bind. The name is the
	// load-bearing part: a named volume survives `docker compose up` recreate
	// of the service container (the upgrade), so the populated store is
	// re-attached rather than discarded.
	namedMount := regexp.MustCompile(`(?m)-\s+([A-Za-z0-9_.-]+):/data(\s|$|:)`)
	m := namedMount.FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("%s: /data is not mounted from a NAMED volume (expected `- <name>:/data`) — "+
			"an anonymous or ephemeral /data is discarded on container recreate, so a "+
			"populated store would NOT survive an upgrade (B8 / R-UPG-05 / INV-DUR-01)", path)
	}
	volName := m[1]

	// That same volume name must be DECLARED in the top-level `volumes:` block
	// (a named volume Docker manages independently of the container lifecycle).
	if !regexp.MustCompile(`(?m)^volumes:\s*$`).MatchString(src) {
		t.Fatalf("%s: no top-level `volumes:` block declaring the durable /data volume (B8 / R-UPG-05)", path)
	}
	declared := regexp.MustCompile(`(?m)^\s{2}` + regexp.QuoteMeta(volName) + `:\s*$`)
	if !declared.MatchString(src) {
		t.Errorf("%s: /data uses volume %q but it is not declared in the top-level `volumes:` "+
			"block — only a declared named volume is durable across container recreate (B8 / R-UPG-05)",
			path, volName)
	}
}
