//go:build integration

// Collector TYPE-contract verification against a live PostgreSQL —
// the #320 audit that locks the PostgreSQL type classes beyond
// internal-"char".
//
// The internal-"char" bug (#312: pg_constraints_v1.contype serialized
// as the integer 102 instead of the string "f", so Analyzer's
// `contype == "f"` skipped every FK — Elevarq/Analyzer#1871) was one
// member of a family. pgx maps each PostgreSQL type to a Go type;
// queryToMaps stores whatever that mapping produced; encoding/json then
// serializes it. A type whose exported JSON form the Analyzer misreads
// is a silent contract violation exactly like "char". "char" is fixed
// centrally (#319 — an OID-18 TextCodec on every pooled connection, and
// regression-locked by TestIntegration_CollectorOutputContractAgainstRealPG);
// this test audits and locks the REST:
//
//   - numeric/decimal — pgx pgtype.Numeric marshals as a JSON NUMBER
//     (a NaN numeric marshals as the string "NaN" — JSON has no NaN);
//   - jsonb/json — pgx map[string]any / []any marshals as a JSON
//     OBJECT/ARRAY, never a base64 []byte string;
//   - bytea — a []byte marshals as a base64 STRING (no collector emits
//     a bytea column, so this class is recorded not-exercised);
//   - arrays (text[], int[]) — pgx []any marshals as a JSON ARRAY,
//     never a Postgres array literal "{a,b}"; EXCEPT the FDW option
//     columns which the redaction post-processor deliberately renders
//     as a map[string]string OBJECT (a contract the Analyzer ingestion
//     test pins — internal/collector/fdw_postprocess.go);
//   - timestamps (timestamptz/timestamp/date) — pgx time.Time marshals
//     as an RFC3339 STRING the Analyzer parses with time.Parse;
//   - oid — pgx uint32 marshals as a JSON NUMBER;
//     regclass/regproc/regnamespace cast to text marshal as a STRING;
//   - bool — pgx bool marshals as JSON true/false, never "t"/"f".
//
// Measured conclusion (verified live on PG 14–18): every one of these
// classes ALREADY serializes in the Analyzer-expected form — the "char"
// class was uniquely broken because pgx's default QCharCodec returns an
// integer, whereas all the others have correct pgx codecs. So #320 adds
// no production SQL change; it is a REGRESSION LOCK plus an explicit
// type-class coverage map so no class can silently drift (OC-R009 /
// OC-R010). The Analyzer-side expectations were cross-checked against
// the live consumer code (ingestion.go toFloat64/relidToInt64,
// wait_event_concentration.go map parsing, ext_stats.go []any coercion,
// replay/sql.go asBool/asString, autovacuum rule RFC3339 parsing).
//
// This test SHARES the #314/#316/#326 fixture (seedRepresentativeSchema)
// and the exported-ZIP reader helpers (readRuns/readResults/keysOf,
// exportedRun/exportedResult) defined in
// signals_collector_output_contract_integration_test.go — same package,
// same seeded schema, so the two harnesses stay in lockstep. It runs its
// own collection cycle against a fresh store so it is independently
// runnable.
//
// Gated by the `integration` build tag and SIGNALS_TEST_PG_DSN (skips
// locally when unset). In CI it runs against a postgres service
// container matrixed PG 14/15/16/17/18.
//
// Run with:
//
//	SIGNALS_TEST_PG_DSN="postgres://signals@localhost/postgres" \
//	  go test -tags integration ./tests/ \
//	  -run TestIntegration_CollectorTypeContractAgainstRealPG
//
// Spec: specifications/collector-output-contract.md (OC-R009, OC-R010).

package tests

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/export"
)

// jsonClass is the exported-JSON shape an audited type class must decode
// to after the export ZIP is read back through encoding/json.
type jsonClass int

const (
	classNumber    jsonClass = iota // JSON number  → Go float64
	classObject                     // JSON object  → Go map[string]any
	classArray                      // JSON array   → Go []any
	classString                     // JSON string  → Go string
	classBool                       // JSON bool    → Go bool
	classTimestamp                  // JSON string, additionally RFC3339-parseable
)

func (c jsonClass) String() string {
	switch c {
	case classNumber:
		return "number"
	case classObject:
		return "object"
	case classArray:
		return "array"
	case classString:
		return "string"
	case classBool:
		return "bool"
	case classTimestamp:
		return "RFC3339-string"
	default:
		return "?"
	}
}

// typeAssertion is one asserted type×collector combination: the audited
// PostgreSQL type class, the collector that emits it, the exported
// column, and the JSON shape the Analyzer consumer expects. capGated
// marks a combination whose collector only emits when the optional FDW
// capability is provisioned (SIGNALS_TEST_PG_SUPERUSER_DSN); absent the
// capability it is not asserted (and not counted as a vacuous failure),
// mirroring OC-R007.
type typeAssertion struct {
	class     string // audited PostgreSQL type class label (for the coverage map)
	collector string // collector ID (query_id)
	column    string // exported column name (SQL alias)
	want      jsonClass
	capGated  bool // true → only when the FDW capability is provisioned
}

// auditedTypeAssertions is the #320 audit matrix. Every combination
// below was verified to emit a non-null value against the seeded schema
// on a live PG 14–18 (measured, not guessed — the FDW "array→object"
// nuance and the pg_policies.permissive "text not bool" nuance were both
// caught by measurement). Each row asserts OC-R009 for its class.
//
// The `class` label groups combinations for the OC-R010 coverage map;
// every audited class beyond "char" appears here at least once, or is
// recorded in notExercisedTypeClasses with a reason.
var auditedTypeAssertions = []typeAssertion{
	// --- numeric/decimal → JSON number ---
	{"numeric", "bloat_estimate_v1", "bloat_ratio", classNumber, false},
	{"numeric", "index_bloat_estimate_v1", "bloat_ratio", classNumber, false},
	{"numeric", "connection_utilization_v1", "pct_used", classNumber, false},
	{"numeric", "planner_stats_staleness_v1", "estimate_drift_pct", classNumber, false},
	{"numeric", "vacuum_health_v1", "dead_pct", classNumber, false},

	// --- jsonb/json → JSON object ---
	{"jsonb", "pg_stat_activity_summary_v1", "by_backend_type", classObject, false},
	{"jsonb", "pg_stat_activity_summary_v1", "by_wait_event_type", classObject, false},
	{"jsonb", "pg_role_capabilities_v1", "role_attrs", classObject, false},

	// --- arrays (text[]/int[]) → JSON array ---
	{"array", "pg_types_v1", "enum_labels", classArray, false},
	{"array", "pg_types_v1", "composite_columns", classArray, false},
	{"array", "index_health_summary_v1", "column_set", classArray, false},
	{"array", "pg_statistic_ext_v1", "attnums", classArray, false},
	{"array", "pg_statistic_ext_v1", "kinds", classArray, false},

	// --- FDW option arrays rendered to a redaction OBJECT (documented,
	// contract-pinned by the Analyzer ingestion test). Capability-gated:
	// fdw_wrappers_v1/fdw_servers_v1 need the postgres_fdw extension +
	// server that only the optional superuser DSN can provision. ---
	{"fdw-option-object", "fdw_wrappers_v1", "fdw_options", classObject, true},
	{"fdw-option-object", "fdw_servers_v1", "server_options", classObject, true},
	{"fdw-option-object", "fdw_foreign_tables_v1", "foreign_table_options", classObject, true},

	// --- timestamps → RFC3339 string ---
	{"timestamp", "cluster_identity_v1", "postmaster_start_time", classTimestamp, false},
	{"timestamp", "server_identity_v1", "started_at", classTimestamp, false},
	{"timestamp", "pg_stat_wal_v1", "stats_reset", classTimestamp, false},
	{"timestamp", "vacuum_health_v1", "last_analyze", classTimestamp, false},
	{"timestamp", "pg_stat_user_tables_v1", "last_analyze", classTimestamp, false},

	// --- oid → JSON number ---
	{"oid", "pg_class_storage_v1", "relid", classNumber, false},
	{"oid", "pg_attribute_storage_v1", "atttypid", classNumber, false},
	{"oid", "index_health_summary_v1", "index_oid", classNumber, false},
	{"oid", "database_sizes_v1", "datid", classNumber, false},
	{"oid", "bloat_estimate_v1", "table_oid", classNumber, false},

	// --- bool → JSON true/false ---
	{"bool", "pg_class_storage_v1", "relhasindex", classBool, false},
	{"bool", "pg_class_storage_v1", "relispartition", classBool, false},
	{"bool", "index_health_summary_v1", "is_unique", classBool, false},
	{"bool", "pg_functions_v1", "security_definer", classBool, false},
	{"bool", "pg_role_capabilities_v1", "is_superuser", classBool, false},
	{"bool", "pg_constraints_v1", "is_validated", classBool, false}, // #342 convalidated → NOT VALID signal
}

// notExercisedTypeClasses records the audited type classes for which no
// collector column can be asserted in this harness, each with a reason —
// the OC-R010 honest-accounting counterpart to auditedTypeAssertions, so
// no audited class is a silent gap.
var notExercisedTypeClasses = map[string]string{
	"bytea": "no registered collector emits a bytea column (a []byte would " +
		"serialize as a base64 string, the Analyzer-expected form, but there " +
		"is nothing to seed or assert)",
	"regclass-text": "regclass/regproc/regnamespace-cast-to-text columns " +
		"(e.g. timescaledb_extension_v1.extension_schema) exist only on " +
		"capability-gated collectors (TimescaleDB not installed in the " +
		"matrix); the string-vs-name fidelity is covered where those " +
		"collectors run, and OC-R008 accounts for their absence here",
}

func TestIntegration_CollectorTypeContractAgainstRealPG(t *testing.T) {
	dsn := os.Getenv("SIGNALS_TEST_PG_DSN")
	if dsn == "" {
		// M0 PostgreSQL lock (#528): the CI integration-pg job sets
		// SIGNALS_REQUIRE_PG=1, so a missing DSN is a hard FAILURE there —
		// PostgreSQL can never be silently absent from a Signals test run.
		// With the flag unset (local inner-loop), it still skips.
		if os.Getenv("SIGNALS_REQUIRE_PG") != "" {
			t.Fatal("SIGNALS_REQUIRE_PG is set but SIGNALS_TEST_PG_DSN is empty — PostgreSQL is mandatory in the CI gate (#528)")
		}
		t.Skip("SIGNALS_TEST_PG_DSN not set — skipping live PostgreSQL integration test")
	}

	// Reuse the #314/#316/#326 fixture (same package) so both harnesses
	// exercise identical representative schema. fdwSeeded gates the FDW
	// option-object combinations (OC-R009 capability-gated leg).
	fdwSeeded := seedRepresentativeSchema(t, dsn)

	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse SIGNALS_TEST_PG_DSN: %v", err)
	}

	passwordEnv := os.Getenv("SIGNALS_TEST_PG_PASSWORD_ENV")
	if passwordEnv == "" && connCfg.Password != "" {
		passwordEnv = "SIGNALS_TC_TEST_PG_PASSWORD"
		t.Setenv(passwordEnv, connCfg.Password)
	}

	sslMode := connCfg.RuntimeParams["sslmode"]
	if sslMode == "" {
		if connCfg.TLSConfig == nil {
			sslMode = "disable"
		} else {
			sslMode = "prefer"
		}
	}

	tgt := config.TargetConfig{
		Name:        "type-contract-target",
		Host:        connCfg.Host,
		Port:        int(connCfg.Port),
		DBName:      connCfg.Database,
		User:        connCfg.User,
		SSLMode:     sslMode,
		PasswordEnv: passwordEnv,
		Enabled:     true,
	}

	store := openTestDB(t)

	c := collector.New(
		store,
		[]config.TargetConfig{tgt},
		24*time.Hour,
		30,
		collector.WithTargetTimeout(90*time.Second),
		collector.WithQueryTimeout(20*time.Second),
		// HighSensitivity on so the definition-mode collectors run too —
		// the seeded schema carries no secrets, matching the sibling harness.
		collector.WithHighSensitivityCollectors(true),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	if !waitForSnapshotCount(t, store, 1, 120*time.Second) {
		cancel()
		<-done
		t.Fatalf("initial collection did not produce a snapshot within 120s (connectivity / role-safety failure?)")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("collector goroutine did not stop within 15s of context cancel")
	}

	// Export via the production path and read the ZIP back — the type the
	// harness inspects is exactly the JSON type a consumer receives (INV-08).
	builder := export.NewBuilder(store, "type-contract-test")
	var buf bytes.Buffer
	if err := builder.WriteTo(&buf, export.Options{}); err != nil {
		t.Fatalf("export WriteTo: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	runs := readRuns(t, zr)
	results := readResults(t, zr)
	if len(runs) == 0 {
		t.Fatal("exported snapshot carries no query_runs — collection produced nothing")
	}

	resultByRunID := map[string]exportedResult{}
	for _, r := range results {
		resultByRunID[r.RunID] = r
	}
	payloadByQueryID := map[string][]map[string]any{}
	for _, run := range runs {
		if run.Status != "success" {
			continue
		}
		if res, ok := resultByRunID[run.ID]; ok {
			payloadByQueryID[run.QueryID] = res.Payload
		}
	}

	// --- OC-R009: each audited type×collector combination decodes to the
	// Analyzer-expected JSON shape, exercised by a seeded non-null value. ---
	assertedByClass := map[string]int{}
	for _, a := range auditedTypeAssertions {
		if a.capGated && !fdwSeeded {
			t.Logf("OC-R009: %s.%s (%s) — FDW capability absent, combination skipped (documented capability gate)",
				a.collector, a.column, a.class)
			continue
		}
		payload, ok := payloadByQueryID[a.collector]
		if !ok {
			t.Errorf("OC-R009 (%s): collector %s produced no successful payload against the seeded schema — cannot verify %q is a %s",
				a.class, a.collector, a.column, a.want)
			continue
		}
		seen := 0
		for i, row := range payload {
			val, present := row[a.column]
			if !present || val == nil {
				continue
			}
			seen++
			if !valueMatchesClass(t, a, i, val) {
				continue
			}
		}
		if seen == 0 {
			t.Errorf("OC-R009 (%s): collector %s emitted no non-null %q value — the seed should have exercised it; assertion would be vacuous (FC-06). keys present in row0: %v",
				a.class, a.collector, a.column, firstRowKeys(payload))
			continue
		}
		assertedByClass[a.class]++
	}

	// --- OC-R010: every audited type class is accounted for. ---
	// Collect the full set of audited classes (asserted + not-exercised).
	classesSeen := map[string]bool{}
	for _, a := range auditedTypeAssertions {
		classesSeen[a.class] = true
	}
	for cls := range notExercisedTypeClasses {
		classesSeen[cls] = true
	}

	var classList []string
	for cls := range classesSeen {
		classList = append(classList, cls)
	}
	sort.Strings(classList)

	for _, cls := range classList {
		asserted := assertedByClass[cls]
		reason, notExercised := notExercisedTypeClasses[cls]
		switch {
		case asserted > 0:
			t.Logf("OC-R010 coverage: type class %-18s ASSERTED (%d combination(s))", cls, asserted)
		case notExercised:
			t.Logf("OC-R010 coverage: type class %-18s not-exercised (%s)", cls, reason)
		default:
			// A class present in the audit matrix but with zero asserted
			// combinations and no not-exercised reason. For a capability-gated
			// class (fdw-option-object) absent the capability this is expected;
			// log it rather than fail — the capability gate is the documented
			// reason. Any OTHER class here is an unclassified gap (FC-07).
			if cls == "fdw-option-object" && !fdwSeeded {
				t.Logf("OC-R010 coverage: type class %-18s not-exercised (FDW capability absent — SIGNALS_TEST_PG_SUPERUSER_DSN unset)", cls)
				continue
			}
			t.Errorf("OC-R010: audited type class %q has zero asserted combinations and no not-exercised reason — classify it (seed a fixture or record a reason), no silent type-class gap (FC-07)", cls)
		}
	}
}

// valueMatchesClass asserts that a single exported value matches the JSON
// class the Analyzer expects for its type class, and reports (via t) a
// descriptive failure otherwise. Returns true on a match.
func valueMatchesClass(t *testing.T, a typeAssertion, rowIdx int, val any) bool {
	t.Helper()
	fail := func(msg string) bool {
		t.Errorf("OC-R009 (%s): %s row %d column %q is a %T (%v) — %s (want %s; the general case of the #312 contype bug)",
			a.class, a.collector, rowIdx, a.column, val, val, msg, a.want)
		return false
	}
	switch a.want {
	case classNumber:
		// pgx numeric/oid → JSON number → Go float64. A NaN numeric is the
		// one documented exception: JSON has no NaN, so pgtype.Numeric
		// marshals it as the string "NaN".
		if _, ok := val.(float64); ok {
			return true
		}
		if s, ok := val.(string); ok && s == "NaN" {
			return true // documented numeric-NaN exception
		}
		return fail("expected a JSON number (a bare number, not an object or a string) — a numeric object or a non-NaN string is the wrong shape")
	case classObject:
		if _, ok := val.(map[string]any); ok {
			return true
		}
		return fail("expected a JSON object (a decoded jsonb/map, not a base64 []byte string)")
	case classArray:
		if _, ok := val.([]any); ok {
			return true
		}
		return fail("expected a JSON array (pgx []any, not a Postgres array literal string \"{a,b}\")")
	case classString:
		if _, ok := val.(string); ok {
			return true
		}
		return fail("expected a JSON string")
	case classBool:
		if _, ok := val.(bool); ok {
			return true
		}
		return fail("expected a JSON bool true/false (not the Postgres \"t\"/\"f\" text form)")
	case classTimestamp:
		s, ok := val.(string)
		if !ok {
			return fail("expected an RFC3339 timestamp string (not a Unix-epoch number)")
		}
		if _, err := time.Parse(time.RFC3339, s); err == nil {
			return true
		}
		if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return true
		}
		return fail(fmt.Sprintf("expected an RFC3339-parseable timestamp string, got %q which time.Parse rejects", s))
	default:
		return fail("unknown expected class")
	}
}

func firstRowKeys(payload []map[string]any) []string {
	if len(payload) == 0 {
		return nil
	}
	return keysOf(payload[0])
}
