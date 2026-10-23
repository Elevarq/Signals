//go:build integration

// Collector output-contract verification against a live PostgreSQL —
// the missing STDD tests->artifact link for issue #314.
//
// The pgqueries unit tests are static (registration, read-only linting,
// ordering); the collector SQL is never run, so the entire internal-
// "char" class defect (#312) was invisible until it surfaced live —
// pg_constraints_v1.contype serialized as the integer 102 instead of
// the string "f", so the Analyzer's `contype == "f"` check skipped
// every FK constraint (Elevarq/Analyzer#1871).
//
// This test closes that gap: it seeds representative schema (a parent +
// child table with an UNINDEXED foreign key) into a real PG, runs the
// full collection, exports a snapshot ZIP via the production
// export.Builder path, reads it back, and asserts the per-collector
// output contract for the catalog/schema collectors:
//
//   - OC-R003 / INV-CHAR-TEXT-VERIFIED: internal-"char" columns
//     (contype, relkind, relpersistence, provolatile, prokind) decode as
//     single-character STRINGS, never JSON numbers; the seeded FK yields
//     a pg_constraints_v1 row with contype == "f" (the #312 lock).
//   - OC-R002 / INV-OUTPUT-CONTRACT: each char-type catalog collector's
//     spec-declared columns are present in its payload objects.
//   - OC-R004 / INV-STATUS-PAYLOAD-VERIFIED: every exported success run
//     with row_count=N has exactly one joinable payload with N rows.
//   - OC-R005 / INV-04 (#319): OID 18 is normalized to text at the
//     connection boundary for ALL collectors — the columns #313's
//     per-column ::text sweep missed (relkind in the bloat collectors,
//     provolatile AS volatility in pg_functions_v1, attidentity in
//     pg_identity_columns_v1) decode as STRINGS, never JSON numbers.
//   - OC-R006 / INV-05 (#326): the remaining internal-"char" schema
//     collectors #314 missed — pg_partitions_v1.partition_strategy,
//     pg_triggers_v1/_definitions_v1.tg_enabled, and
//     pg_functions_v1/_definitions_v1.volatility — decode as single-char
//     STRINGS. A seeded partitioned table, trigger, and function make
//     each emit rows.
//   - OC-R007 / INV-05 (#326, capability-gated): when a superuser DSN
//     (SIGNALS_TEST_PG_SUPERUSER_DSN) provisions postgres_fdw + a server,
//     a seeded foreign table makes fdw_foreign_tables_v1.relkind decode
//     as the string "f"; absent the capability the assertion is skipped
//     with a documented reason.
//
// Gated by the `integration` build tag and SIGNALS_TEST_PG_DSN (skips
// locally when unset), matching signals_integration_test.go. In CI it
// runs against a postgres service container matrixed PG 14/15/16/17/18.
//
// Run with:
//
//	SIGNALS_TEST_PG_DSN="postgres://signals@localhost/postgres" \
//	  go test -tags integration ./tests/ \
//	  -run TestIntegration_CollectorOutputContractAgainstRealPG
//
// Spec: specifications/collector-output-contract.md (OC-R001..R005);
//       features/signals/appendix-a-api-contract.md
//       §"Collector output-contract verification (#314)".

package tests

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/config"
	"github.com/elevarq/signals/internal/export"
	"github.com/elevarq/signals/internal/pgqueries"
)

// internalCharColumns are the PostgreSQL internal-"char" columns the
// #313 fix cast ::text. pgx scans an uncast "char" as an integer byte,
// so any of these appearing as a JSON number in a payload is the #312
// regression. Names are the exported column ALIASES in the collector
// SQL — the whitelist keys the OUTPUT column name, so the function
// volatility field is "volatility" (its alias), never "provolatile".
//
// #326 adds the aliases the remaining schema collectors expose but the
// #314 fixtures never exercised: partition_strategy (pg_partitions_v1,
// from partstrat), tg_enabled (pg_triggers_v1 /
// pg_triggers_definitions_v1, from tgenabled), volatility
// (pg_functions_v1 / pg_functions_definitions_v1, the OUTPUT alias of
// provolatile), and attidentity (pg_identity_columns_v1). relkind
// already covers fdw_foreign_tables_v1 once that collector is seeded.
var internalCharColumns = map[string]bool{
	"contype":            true,
	"relkind":            true,
	"relpersistence":     true,
	"provolatile":        true,
	"prokind":            true,
	"partition_strategy": true, // pg_partitions_v1 (from partstrat) — #326
	"tg_enabled":         true, // pg_triggers_v1 / _definitions_v1 (from tgenabled) — #326
	"volatility":         true, // pg_functions_v1 / _definitions_v1 (OUTPUT alias of provolatile) — #326
	// NOTE: attidentity is deliberately NOT in this map. The non-set
	// identity case is the empty string "" (a legitimate, non-numeric
	// value), which the generic single-char sweep below would reject.
	// It is instead asserted by the dedicated assertStringCol(...,
	// allowEmpty=true) call for pg_identity_columns_v1 (OC-R005). #326.
}

// declaredColumnsByCollector is the subset of spec-declared columns
// (specifications/collectors/<id>.md "Output columns") asserted present
// for the internal-"char" catalog collectors touched by #313. Kept to
// the stable structural columns so version-gated additions don't make
// the test brittle. OC-R002 / INV-OUTPUT-CONTRACT.
var declaredColumnsByCollector = map[string][]string{
	"pg_constraints_v1": {
		"schemaname", "relname", "conname", "contype", "condef",
		"column_name", "column_position", "relkind", "is_validated",
	},
	"pg_class_storage_v1": {
		"relid", "schemaname", "relname", "relkind", "relpersistence",
		"relispartition", "relhasindex",
	},
	// #326 — the remaining internal-"char" schema collectors the
	// representative fixture now makes emit rows. Columns per each
	// collector's spec "Output columns" table (kept to the always-present
	// structural set + the aliased char column under test).
	"pg_partitions_v1": {
		"parent_schema", "parent_name", "partition_strategy",
		"partition_key", "child_schema", "child_name", "child_bounds",
	},
	"pg_triggers_v1": {
		"schemaname", "relname", "tgname", "tgtype",
		"tg_funcschema", "tg_funcname", "tg_enabled",
	},
	"pg_functions_v1": {
		"schemaname", "proname", "identity_args", "return_type",
		"language", "volatility", "security_definer", "is_strict",
		"prokind",
	},
	"pg_identity_columns_v1": {
		"schemaname", "relname", "attname", "atttypname", "attidentity",
		"atthasdef", "default_is_nextval", "auto_owned_sequence",
		"is_primary_key", "is_unique",
	},
}

// fdwForeignTablesDeclaredColumns is asserted only when the FDW
// capability is available (OC-R007) — the collector emits no rows and
// no payload without a seeded foreign table, so it cannot live in the
// unconditional declaredColumnsByCollector map.
var fdwForeignTablesDeclaredColumns = []string{
	"schemaname", "table_name", "table_oid", "relkind",
	"server_name", "fdw_name", "foreign_table_options",
}

// exportedRun mirrors the query_runs.ndjson row shape emitted by
// export.Builder.writeQueryRuns.
type exportedRun struct {
	ID       string `json:"id"`
	QueryID  string `json:"query_id"`
	RowCount int    `json:"row_count"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Error    string `json:"error"`
}

// exportedResult mirrors the query_results.ndjson row shape emitted by
// export.Builder.writeQueryResults: run_id + decoded payload objects.
type exportedResult struct {
	RunID   string           `json:"run_id"`
	Payload []map[string]any `json:"payload"`
}

func TestIntegration_CollectorOutputContractAgainstRealPG(t *testing.T) {
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

	fdwSeeded := seedRepresentativeSchema(t, dsn)

	connCfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse SIGNALS_TEST_PG_DSN: %v", err)
	}

	// Collection reads the target password from the env var named by
	// PasswordEnv (config.TargetConfig). Prefer an operator-supplied
	// var name; otherwise carry the password from the DSN through a
	// dedicated var so the collector authenticates the same way the
	// seed did. Peer/trust auth leaves both empty and still works.
	passwordEnv := os.Getenv("SIGNALS_TEST_PG_PASSWORD_ENV")
	if passwordEnv == "" && connCfg.Password != "" {
		passwordEnv = "SIGNALS_OC_TEST_PG_PASSWORD"
		t.Setenv(passwordEnv, connCfg.Password)
	}

	// sslmode from the DSN when present (a CI service container is
	// plaintext -> disable); default to prefer so a TLS-capable target
	// still negotiates TLS.
	sslMode := connCfg.RuntimeParams["sslmode"]
	if sslMode == "" {
		if connCfg.TLSConfig == nil {
			sslMode = "disable"
		} else {
			sslMode = "prefer"
		}
	}

	tgt := config.TargetConfig{
		Name:        "output-contract-target",
		Host:        connCfg.Host,
		Port:        int(connCfg.Port),
		DBName:      connCfg.Database,
		User:        connCfg.User,
		SSLMode:     sslMode,
		PasswordEnv: passwordEnv,
		Enabled:     true,
	}

	store := openTestDB(t)

	// Run's immediate baseline cycle executes every eligible collector
	// against a fresh store (no prior run has consumed the daily
	// cadence), so the catalog/schema collectors we assert on all fire
	// on the first cycle.
	c := collector.New(
		store,
		[]config.TargetConfig{tgt},
		24*time.Hour,
		30,
		collector.WithTargetTimeout(90*time.Second),
		collector.WithQueryTimeout(20*time.Second),
		// #326: enable the HighSensitivity definition-text collectors so
		// pg_triggers_definitions_v1 and pg_functions_definitions_v1 run
		// and their tg_enabled / volatility aliases are asserted too
		// (they are off by default). The seeded schema carries no
		// secrets, so exercising the definition bodies is safe here.
		collector.WithHighSensitivityCollectors(true),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	// Run kicks off an immediate baseline cycle. Wait for it to land a
	// snapshot rather than sleeping a fixed duration.
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

	// Export a snapshot ZIP via the production path and read it back.
	builder := export.NewBuilder(store, "output-contract-test")
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

	// --- OC-R004 / INV-STATUS-PAYLOAD-VERIFIED ---------------------
	// Every success run with row_count=N joins to exactly one payload
	// with exactly N objects. This is the end-to-end proof of
	// INV-SNAP-STATUS-PAYLOAD (#312) through the real export.
	resultByRunID := map[string]exportedResult{}
	for _, r := range results {
		if _, dup := resultByRunID[r.RunID]; dup {
			t.Errorf("OC-R004: duplicate query_results payload for run_id %s", r.RunID)
		}
		resultByRunID[r.RunID] = r
	}
	for _, run := range runs {
		if run.Status != "success" {
			continue
		}
		res, ok := resultByRunID[run.ID]
		if !ok {
			t.Errorf("OC-R004 (INV-STATUS-PAYLOAD): success run %s (query_id=%s, row_count=%d) has NO joinable query_results payload",
				run.ID, run.QueryID, run.RowCount)
			continue
		}
		if len(res.Payload) != run.RowCount {
			t.Errorf("OC-R004 (INV-STATUS-PAYLOAD): run %s (query_id=%s) declares row_count=%d but payload has %d rows",
				run.ID, run.QueryID, run.RowCount, len(res.Payload))
		}
	}

	// Index successful catalog/schema collector payloads by query_id.
	payloadByQueryID := map[string][]map[string]any{}
	for _, run := range runs {
		if run.Status != "success" {
			continue
		}
		if res, ok := resultByRunID[run.ID]; ok {
			payloadByQueryID[run.QueryID] = res.Payload
		}
	}

	// --- OC-R003 / INV-CHAR-TEXT-VERIFIED --------------------------
	// Across ALL collector payloads, every internal-"char" column that
	// appears must decode as a single-character STRING, never a number.
	charSeen := 0
	for queryID, payload := range payloadByQueryID {
		for i, row := range payload {
			for col, val := range row {
				if !internalCharColumns[col] {
					continue
				}
				if val == nil {
					continue // NULLs are legitimate
				}
				charSeen++
				s, isStr := val.(string)
				if !isStr {
					t.Errorf("OC-R003 (INV-CHAR-TEXT): %s row %d column %q is a %T (%v), want single-char string — the #312 regression (pgx scans \"char\" as int)",
						queryID, i, col, val, val)
					continue
				}
				if len(s) != 1 {
					t.Errorf("OC-R003 (INV-CHAR-TEXT): %s row %d column %q = %q, want a single character",
						queryID, i, col, s)
				}
			}
		}
	}
	if charSeen == 0 {
		t.Error("OC-R003: no internal-\"char\" columns observed in any payload — the seeded schema should have produced pg_constraints_v1/pg_class_storage_v1 rows; harness is not exercising the #312 class")
	}

	// --- OC-R003 (the exact #312 lock): the seeded unindexed FK MUST
	// produce a pg_constraints_v1 row with contype == "f" (string). ---
	constraints, ok := payloadByQueryID["pg_constraints_v1"]
	if !ok {
		t.Fatal("OC-R003: pg_constraints_v1 produced no successful payload — cannot verify the #312 contype==\"f\" lock")
	}
	foundFK := false
	for _, row := range constraints {
		if ct, _ := row["contype"].(string); ct == "f" {
			foundFK = true
			break
		}
	}
	if !foundFK {
		t.Errorf("OC-R003 (#312 LOCK): no pg_constraints_v1 row with contype==\"f\" for the seeded unindexed FK; contype values seen=%v — this is exactly the #312 regression (uncast contype serialized as 102)",
			distinctContype(constraints))
	}

	// --- #342: pg_constraints_v1.is_validated mirrors pg_constraint
	// .convalidated. The two seeded NOT VALID constraints MUST surface
	// is_validated == false; the validated PK/FK above MUST surface true.
	// This proves the NOT-VALID signal end to end so a consumer reads the
	// clean catalog boolean instead of string-parsing condef (Analyzer
	// #1759). is_validated marshals as a JSON bool (never "t"/"f"). ---
	validity := map[string]bool{}
	sawValidatedTrue := false
	for _, row := range constraints {
		name, _ := row["conname"].(string)
		v, isBool := row["is_validated"].(bool)
		if !isBool {
			t.Errorf("#342: pg_constraints_v1 row %q is_validated is %T (%v), want a JSON bool",
				name, row["is_validated"], row["is_validated"])
			continue
		}
		validity[name] = v
		if v {
			sawValidatedTrue = true
		}
	}
	for _, nv := range []string{"child_amount_nonneg_nv", "child_alt_parent_fk_nv"} {
		got, present := validity[nv]
		if !present {
			t.Errorf("#342: seeded NOT VALID constraint %q missing from pg_constraints_v1 payload (conkey should have emitted a row)", nv)
			continue
		}
		if got {
			t.Errorf("#342: constraint %q was added NOT VALID but is_validated=true — want false (convalidated must be false)", nv)
		}
	}
	if !sawValidatedTrue {
		t.Error("#342: no pg_constraints_v1 row with is_validated=true — the seeded validated PK/FK should surface convalidated=true")
	}

	// --- OC-R005 / INV-04 (the #319 central-fix lock) --------------
	// The columns #313's per-column ::text sweep left uncast must ALSO
	// decode as strings, proving OID 18 is normalized at the connection
	// boundary for ALL collectors, not just the hand-cast ones:
	//
	//   - relkind in the bloat collectors (single-char, e.g. "r"/"i"),
	//   - provolatile AS volatility in pg_functions_v1 (single-char,
	//     e.g. "i"/"s"/"v"),
	//   - attidentity in pg_identity_columns_v1 (a string; "" for the
	//     non-set case, "a"/"d" when set — never a number).
	//
	// These assertions go RED with the OID-18 AfterConnect registration
	// removed (volatility/attidentity would decode as numbers) and GREEN
	// with it in place.
	assertStringCol := func(queryID, col string, allowEmpty bool) {
		payload, ok := payloadByQueryID[queryID]
		if !ok {
			t.Errorf("OC-R005 (#319): expected collector %s to produce a payload against the seeded schema, but none present — cannot verify %q normalizes to a string", queryID, col)
			return
		}
		seen := 0
		for i, row := range payload {
			val, present := row[col]
			if !present || val == nil {
				continue
			}
			seen++
			s, isStr := val.(string)
			if !isStr {
				t.Errorf("OC-R005 (#319 BOUNDARY LOCK): %s row %d column %q is a %T (%v), want a string — the internal \"char\" (OID 18) is NOT normalized at the connection boundary (this column was missed by #313's per-column ::text sweep)",
					queryID, i, col, val, val)
				continue
			}
			if len(s) > 1 {
				t.Errorf("OC-R005 (#319): %s row %d column %q = %q, want a single character (or empty)", queryID, i, col, s)
			}
			if len(s) == 0 && !allowEmpty {
				t.Errorf("OC-R005 (#319): %s row %d column %q is empty, want a single character", queryID, i, col)
			}
		}
		if seen == 0 {
			t.Errorf("OC-R005 (#319): collector %s emitted no %q values — the seeded schema should have exercised it; harness is not covering the columns #313 missed", queryID, col)
		}
	}
	// relkind in the bloat collectors: single-char, non-empty.
	assertStringCol("bloat_estimate_v1", "relkind", false)
	assertStringCol("index_bloat_estimate_v1", "relkind", false)
	// provolatile AS volatility: single-char, non-empty.
	assertStringCol("pg_functions_v1", "volatility", false)
	// attidentity: may be "" (non-set) or a single char (a/d).
	assertStringCol("pg_identity_columns_v1", "attidentity", true)

	// --- OC-R006 / INV-05 (the #326 schema-family char lock) -------
	// The remaining schema collectors that alias an internal-"char"
	// column but the #314 fixtures never exercised. The seeded
	// partitioned table, trigger, and function make each emit a row; the
	// aliased char value must decode as a single, non-empty character
	// string, never a number. These go RED with the OID-18 AfterConnect
	// registration removed and GREEN with it in place.
	//
	//   - partition_strategy (pg_partitions_v1, from partstrat) — 'r' for
	//     the RANGE-partitioned events table,
	//   - tg_enabled (pg_triggers_v1 / _definitions_v1, from tgenabled) —
	//     'O' for the enabled BEFORE INSERT trigger,
	//   - volatility (pg_functions_v1 / _definitions_v1, the OUTPUT alias
	//     of provolatile) — 'i' for the IMMUTABLE function.
	assertStringCol("pg_partitions_v1", "partition_strategy", false)
	assertStringCol("pg_triggers_v1", "tg_enabled", false)
	assertStringCol("pg_triggers_definitions_v1", "tg_enabled", false)
	assertStringCol("pg_functions_v1", "volatility", false)
	assertStringCol("pg_functions_definitions_v1", "volatility", false)

	// --- OC-R007 / INV-05 (the #326 FDW leg, capability-gated) ------
	// fdw_foreign_tables_v1.relkind is always 'f' (a single char). Only
	// asserted when the FDW capability was provisioned (a superuser DSN
	// created the extension + server + granted USAGE); otherwise the
	// fixture was skipped with a documented reason and the collector
	// emits no rows.
	if fdwSeeded {
		assertStringCol("fdw_foreign_tables_v1", "relkind", false)
		// The FDW collector's declared columns are present in its payload
		// (OC-R002 extended to the capability-gated collector).
		if payload, ok := payloadByQueryID["fdw_foreign_tables_v1"]; ok && len(payload) > 0 {
			row := payload[0]
			for _, col := range fdwForeignTablesDeclaredColumns {
				if _, present := row[col]; !present {
					t.Errorf("OC-R002 (INV-OUTPUT-CONTRACT): collector fdw_foreign_tables_v1 payload is missing declared column %q; keys present=%v",
						col, keysOf(row))
				}
			}
		} else {
			t.Error("OC-R007: FDW capability was provisioned but fdw_foreign_tables_v1 produced no payload — the seeded foreign table should have made it emit a row")
		}
	} else {
		t.Log("OC-R007: FDW capability absent — fdw_foreign_tables_v1.relkind assertion skipped (documented capability gate)")
	}

	// --- OC-R002 / INV-OUTPUT-CONTRACT (targeted char collectors) --
	// The #314/#326 char-type catalog collectors keep an explicit,
	// hand-audited declared-column list as a stronger, drift-proof
	// belt-and-braces check alongside the spec-derived sweep below.
	for collectorID, cols := range declaredColumnsByCollector {
		payload, ok := payloadByQueryID[collectorID]
		if !ok {
			t.Errorf("OC-R002: expected collector %s to produce a payload against the seeded schema, but none present", collectorID)
			continue
		}
		if len(payload) == 0 {
			t.Errorf("OC-R002: collector %s produced an empty payload; cannot verify declared columns", collectorID)
			continue
		}
		row := payload[0]
		for _, col := range cols {
			if _, present := row[col]; !present {
				t.Errorf("OC-R002 (INV-OUTPUT-CONTRACT): collector %s payload is missing declared column %q; keys present=%v",
					collectorID, col, keysOf(row))
			}
		}
	}

	// --- OC-R002 / INV-OUTPUT-CONTRACT (spec-derived, ALL collectors) ---
	// #316: extend the per-collector declared-column assertion from the
	// handful of char collectors above to EVERY registered collector.
	// For each collector that emitted rows against the seeded live PG,
	// derive its declared output columns from its spec (parseOutputColumns)
	// and assert each is present in its exported payload objects — closing
	// the gap where a non-schema collector could silently drop or rename a
	// declared column. Collectors that legitimately emit zero rows in this
	// environment are recorded in zeroRowAllowlist (with a reason) rather
	// than silently skipped, so coverage is honest and a newly-added
	// collector cannot slip through unclassified.
	asserted := 0        // collectors whose declared columns were verified against emitted rows
	dynamicAsserted := 0 // column-dynamic collectors verified for row-presence only
	var notExercised []string
	var missing []string // unclassified zero-row/absent collectors (fail)

	for _, q := range pgqueries.All() {
		id := q.ID
		payload, ran := payloadByQueryID[id]
		if ran && len(payload) > 0 {
			cols, dynamic := declaredColumnsForCollector(t, id)
			if dynamic {
				// Column-dynamic (SELECT *) — assert row presence only.
				dynamicAsserted++
				t.Logf("OC-R002: collector %s asserted (column-dynamic, %d rows, row-presence only)", id, len(payload))
				continue
			}
			for _, row := range payload {
				for _, col := range cols {
					if _, present := row[col]; !present {
						t.Errorf("OC-R002 (INV-OUTPUT-CONTRACT): collector %s payload is missing spec-declared column %q; keys present=%v",
							id, col, keysOf(row))
					}
				}
			}
			asserted++
			continue
		}

		// No rows emitted (skipped, not eligible, or 0-row success). It
		// MUST be classified in the zero-row allowlist with a reason —
		// no silent coverage gap.
		reason, ok := zeroRowAllowlist[id]
		if !ok {
			missing = append(missing, id)
			continue
		}
		notExercised = append(notExercised, fmt.Sprintf("%s (%s)", id, reason))
	}

	sort.Strings(notExercised)
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("OC-R002: %d collector(s) emitted no rows and are NOT in zeroRowAllowlist — classify each with a fixture (seed it to emit rows) or an explicit reason, no silent gaps: %v",
			len(missing), missing)
	}

	// Coverage report — the honest accounting the issue asks for.
	total := len(pgqueries.All())
	t.Logf("OC-R002 coverage: %d/%d collectors have declared-column assertions against emitted rows (+%d column-dynamic row-presence); %d not exercised (allowlisted):",
		asserted, total, dynamicAsserted, len(notExercised))
	for _, e := range notExercised {
		t.Logf("  not-exercised: %s", e)
	}
}

// seedRepresentativeSchema creates a parent table (PK) and a child
// table whose FK to the parent is UNINDEXED, plus a non-PK index and
// columns of varied types. The unindexed FK is what makes
// pg_constraint emit a contype='f' row — the exact #312 fixture.
//
// #326 extends it with a partitioned table, a user trigger, and a SQL
// function so the remaining internal-"char" schema collectors emit rows,
// and — when the FDW capability is available — a foreign table. It
// returns whether the FDW fixture was seeded so the caller can
// capability-gate the fdw_foreign_tables_v1.relkind assertion (OC-R007).
func seedRepresentativeSchema(t *testing.T, dsn string) (fdwSeeded bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("seed: connect: %v", err)
	}
	defer pool.Close()

	// Idempotent seed in a dedicated schema so repeated runs are clean
	// and we never touch the target's own objects.
	stmts := []string{
		`DROP SCHEMA IF EXISTS signals_oc_test CASCADE`,
		`CREATE SCHEMA signals_oc_test`,
		`CREATE TABLE signals_oc_test.parent (
			id          bigint PRIMARY KEY,
			label       text NOT NULL,
			created_at  timestamptz NOT NULL DEFAULT now(),
			ratio       double precision,
			flags       boolean DEFAULT false
		)`,
		// Child table: fk_parent_id references parent(id) with NO index
		// on the FK column -> pg_constraint emits a contype='f' row and
		// the analyzer's missing-FK-index rule has a target.
		`CREATE TABLE signals_oc_test.child (
			id          bigint PRIMARY KEY,
			fk_parent_id bigint NOT NULL
				REFERENCES signals_oc_test.parent(id),
			note        varchar(64),
			amount      numeric(12,2)
		)`,
		// A non-PK index on a NON-FK column, so at least one ordinary
		// index exists without accidentally indexing the FK. This heap
		// table + its index also feed the bloat collectors so their
		// relkind columns ('r'/'i') are exercised (OC-R005 / #319).
		`CREATE INDEX child_note_idx ON signals_oc_test.child (note)`,
		// #342 — NOT VALID constraints so pg_constraints_v1.is_validated
		// (convalidated) has a false case alongside the validated PK/FK
		// above (which surface true). A NOT VALID constraint is enforced
		// for new writes but was never checked against existing rows.
		// Both reference a column, so conkey is non-empty and the
		// unnest(conkey) join emits a row for each.
		`ALTER TABLE signals_oc_test.child
			ADD CONSTRAINT child_amount_nonneg_nv CHECK (amount >= 0) NOT VALID`,
		`ALTER TABLE signals_oc_test.child ADD COLUMN alt_parent_id bigint`,
		`ALTER TABLE signals_oc_test.child
			ADD CONSTRAINT child_alt_parent_fk_nv
			FOREIGN KEY (alt_parent_id) REFERENCES signals_oc_test.parent(id) NOT VALID`,
		// An identity column so pg_identity_columns_v1 emits attidentity
		// (the raw "char" column #313's per-column sweep missed). The
		// generated column yields attidentity='a'; the plain int/uuid
		// columns above yield the non-set attidentity='' (empty string
		// under text-protocol semantics) — both are exercised. #319.
		`CREATE TABLE signals_oc_test.identity_demo (
			id    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
			key   uuid NOT NULL,
			label text
		)`,
		// A SQL function so pg_functions_v1 emits provolatile AS
		// volatility (also raw "char", missed by #313). #319.
		`CREATE FUNCTION signals_oc_test.demo_immutable(x integer)
			RETURNS integer LANGUAGE sql IMMUTABLE
			AS 'SELECT x + 1'`,
		// A RANGE-partitioned table with one child partition so
		// pg_partitions_v1 emits a row carrying partition_strategy
		// (partstrat AS partition_strategy — raw "char", 'r' for RANGE).
		// The remaining internal-"char" schema collectors #314 missed.
		// #326.
		`CREATE TABLE signals_oc_test.events (
			id  bigint NOT NULL,
			ts  timestamptz NOT NULL
		) PARTITION BY RANGE (ts)`,
		`CREATE TABLE signals_oc_test.events_2026
			PARTITION OF signals_oc_test.events
			FOR VALUES FROM ('2026-01-01') TO ('2027-01-01')`,
		// A trigger function + a user (non-internal) BEFORE INSERT row
		// trigger so pg_triggers_v1 / pg_triggers_definitions_v1 emit a
		// row carrying tg_enabled (tgenabled AS tg_enabled — raw "char",
		// 'O' for an origin/enabled trigger). #326.
		`CREATE FUNCTION signals_oc_test.tg_noop()
			RETURNS trigger LANGUAGE plpgsql
			AS 'BEGIN RETURN NEW; END'`,
		`CREATE TRIGGER child_bi
			BEFORE INSERT ON signals_oc_test.child
			FOR EACH ROW EXECUTE FUNCTION signals_oc_test.tg_noop()`,
		// #316 — representative fixtures that lift the remaining
		// cheaply-seedable non-schema/schema collectors into the
		// declared-column assertion set (OC-R002). Each object lands in
		// the dedicated schema so collection stays scoped and the seed
		// stays read-only against the target's own objects.
		//
		// A view + materialized view so pg_views_v1 / pg_views_definitions_v1
		// and pg_matviews_v1 / pg_matviews_definitions_v1 emit rows.
		`CREATE VIEW signals_oc_test.parent_view AS
			SELECT id, label FROM signals_oc_test.parent`,
		`CREATE MATERIALIZED VIEW signals_oc_test.parent_mv AS
			SELECT id, label FROM signals_oc_test.parent WITH DATA`,
		`CREATE INDEX parent_mv_idx ON signals_oc_test.parent_mv (id)`,
		// An enum type and a composite type so pg_types_v1 emits rows
		// (it filters to typtype IN ('e','c','d')).
		`CREATE TYPE signals_oc_test.color AS ENUM ('red', 'green', 'blue')`,
		`CREATE TYPE signals_oc_test.point2d AS (x double precision, y double precision)`,
		// An RLS policy on the child table so pg_policies_v1 emits a row.
		`ALTER TABLE signals_oc_test.child ENABLE ROW LEVEL SECURITY`,
		`CREATE POLICY child_all ON signals_oc_test.child
			USING (fk_parent_id > 0)`,
		// Multi-column extended statistics so pg_statistic_ext_v1 emits a
		// row; ANALYZE computes the dependency data.
		`CREATE STATISTICS signals_oc_test.child_stats (dependencies)
			ON fk_parent_id, note FROM signals_oc_test.child`,
		// A function carrying a SET config so pg_proc_config_v1 emits a
		// row (it filters to proconfig IS NOT NULL).
		`CREATE FUNCTION signals_oc_test.demo_config()
			RETURNS integer LANGUAGE sql
			SET search_path = 'pg_catalog'
			AS 'SELECT 1'`,
		// A user rule so pg_rules_v1 emits a row (the implicit view
		// _RETURN rule is excluded by the collector).
		`CREATE TABLE signals_oc_test.rule_log (
			id bigint,
			note text
		)`,
		`CREATE RULE rule_log_noop AS ON UPDATE TO signals_oc_test.rule_log
			DO INSTEAD NOTHING`,
		`INSERT INTO signals_oc_test.parent (id, label, ratio)
			VALUES (1, 'a', 1.5), (2, 'b', 2.5)`,
		`INSERT INTO signals_oc_test.child (id, fk_parent_id, note, amount)
			VALUES (10, 1, 'x', 9.99), (11, 2, 'y', 1.00)`,
		`INSERT INTO signals_oc_test.identity_demo (key, label)
			VALUES (gen_random_uuid(), 'r1'), (gen_random_uuid(), 'r2')`,
		// ANALYZE so pg_stat_user_tables / n_live_tup are populated.
		`ANALYZE signals_oc_test.parent`,
		`ANALYZE signals_oc_test.child`,
		`ANALYZE signals_oc_test.identity_demo`,
	}

	// --- FDW capability (OC-R007) ---------------------------------------
	// fdw_foreign_tables_v1 only emits a row when a foreign table exists.
	// A pg_monitor collector role cannot CREATE EXTENSION postgres_fdw or
	// a FOREIGN DATA WRAPPER itself, so the extension + a foreign SERVER +
	// a USAGE grant are provisioned via an OPTIONAL superuser DSN
	// (SIGNALS_TEST_PG_SUPERUSER_DSN). The foreign table itself is then
	// created by the collector role (pool) so the fixture stays in the
	// dedicated schema. When the capability is unavailable the fixture is
	// skipped and the caller capability-gates the relkind assertion.
	fdwSeeded = provisionFDWCapability(t, ctx)
	if fdwSeeded {
		// The foreign table's remote target need not exist — the collector
		// reads only local catalog metadata (pg_class.relkind='f'), never
		// the remote data (see fdw_foreign_tables_v1.md). #326.
		stmts = append(stmts,
			`CREATE FOREIGN TABLE signals_oc_test.remote_events (
				id bigint,
				ts timestamptz
			) SERVER signals_oc_test_fdw_srv
			  OPTIONS (schema_name 'public', table_name 'nonexistent_ok')`,
		)
	}

	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("seed stmt failed: %v\nSQL: %s", err, s)
		}
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		p, err := pgxpool.New(cctx, dsn)
		if err != nil {
			return
		}
		defer p.Close()
		_, _ = p.Exec(cctx, `DROP SCHEMA IF EXISTS signals_oc_test CASCADE`)
	})
	return fdwSeeded
}

// provisionFDWCapability provisions the postgres_fdw extension, a
// dedicated foreign server, and a USAGE grant to the collector role,
// using the OPTIONAL SIGNALS_TEST_PG_SUPERUSER_DSN superuser connection.
// It reports whether the FDW capability is available for the fixture.
//
// Returns false (documented skip, OC-R007) when the superuser DSN is
// unset or the extension is unavailable — the harness then omits the
// foreign-table fixture and capability-gates fdw_foreign_tables_v1.relkind.
// The provisioning is idempotent and touches only the dedicated server;
// collection itself still runs as the non-superuser collector role
// (INV-02 / INV-06 hold).
func provisionFDWCapability(t *testing.T, ctx context.Context) bool {
	t.Helper()

	superDSN := os.Getenv("SIGNALS_TEST_PG_SUPERUSER_DSN")
	if superDSN == "" {
		t.Log("OC-R007: SIGNALS_TEST_PG_SUPERUSER_DSN unset — skipping FDW fixture (fdw_foreign_tables_v1.relkind assertion is capability-gated)")
		return false
	}

	collectorRole := collectorRoleFromDSN(t)
	if collectorRole == "" {
		t.Log("OC-R007: could not resolve the collector role from SIGNALS_TEST_PG_DSN — skipping FDW fixture")
		return false
	}

	sp, err := pgxpool.New(ctx, superDSN)
	if err != nil {
		t.Logf("OC-R007: superuser connect failed (%v) — skipping FDW fixture", err)
		return false
	}
	defer sp.Close()

	// Idempotent: drop a stale server, (re)create the extension + server,
	// and grant USAGE. CREATE EXTENSION / FOREIGN DATA WRAPPER need
	// superuser — hence the separate DSN.
	superStmts := []string{
		`DROP SERVER IF EXISTS signals_oc_test_fdw_srv CASCADE`,
		`CREATE EXTENSION IF NOT EXISTS postgres_fdw`,
		`CREATE SERVER signals_oc_test_fdw_srv
			FOREIGN DATA WRAPPER postgres_fdw
			OPTIONS (host 'localhost', port '5432', dbname 'postgres')`,
		fmt.Sprintf(`GRANT USAGE ON FOREIGN SERVER signals_oc_test_fdw_srv TO %s`,
			pgx.Identifier{collectorRole}.Sanitize()),
	}
	for _, s := range superStmts {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Logf("OC-R007: FDW provisioning failed (%v) on: %s — skipping FDW fixture", err, s)
			return false
		}
	}

	// Retire the server on cleanup so repeated runs stay clean.
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer ccancel()
		p, err := pgxpool.New(cctx, superDSN)
		if err != nil {
			return
		}
		defer p.Close()
		_, _ = p.Exec(cctx, `DROP SERVER IF EXISTS signals_oc_test_fdw_srv CASCADE`)
	})
	return true
}

// collectorRoleFromDSN returns the role name the collector authenticates
// as, parsed from SIGNALS_TEST_PG_DSN, so the USAGE grant targets exactly
// that role.
func collectorRoleFromDSN(t *testing.T) string {
	t.Helper()
	cfg, err := pgx.ParseConfig(os.Getenv("SIGNALS_TEST_PG_DSN"))
	if err != nil {
		return ""
	}
	return cfg.User
}

func readRuns(t *testing.T, zr *zip.Reader) []exportedRun {
	t.Helper()
	data := readZipFile(t, zr, "query_runs.ndjson")
	var out []exportedRun
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r exportedRun
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("decode query_runs row: %v\nline: %s", err, line)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan query_runs.ndjson: %v", err)
	}
	return out
}

func readResults(t *testing.T, zr *zip.Reader) []exportedResult {
	t.Helper()
	data := readZipFile(t, zr, "query_results.ndjson")
	var out []exportedResult
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r exportedResult
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("decode query_results row: %v", err)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan query_results.ndjson: %v", err)
	}
	return out
}

func distinctContype(rows []map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		v := fmt.Sprintf("%v (%T)", r["contype"], r["contype"])
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
