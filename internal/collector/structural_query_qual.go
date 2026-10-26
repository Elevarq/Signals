package collector

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/elevarq/signals/internal/singlestore"
)

// Live structural-query-text qualification verifier (#647,
// specifications/structural-query-text.md). Shared by the mysql_live and
// mariadb_live legs; each engine/version runs it independently (SQT-R009) so a
// green MySQL leg never qualifies MariaDB and vice versa.
//
// It proves INV-SQT-01 (no runtime literal leakage) on the REAL exported ZIP a
// consumer would read, plus the end-to-end state/redaction/truncation contract.

// structuralAdversarialLiterals are the runtime literal tokens the adversarial
// workload embeds (examples/mysql-init.sql / mariadb-init.sql — single source of
// truth). None may appear in any exported DIGEST_TEXT: the server normalizes
// them to `?` before Signals reads the column. Identifiers (table/column names,
// the sqt_alias alias) are deliberately NOT in this list — they may appear and
// are expected structural evidence, not a leak.
var structuralAdversarialLiterals = []string{
	"sqtlit_str_6f1a2b",
	"918273645",
	"61728.39455",
	"2023-09-14",
	"771001", "771002", "771003",
	"sqtlit_like_7c9",
}

// digestCollectorID returns the digest collector id for a MySQL-wire engine.
func digestCollectorID(engine string) string {
	return engine + "_statement_digests_v1"
}

// VerifyStructuralQueryText checks the structural-opt-in export and the control
// (opt-in off) export for one engine line. Returns a non-nil error describing
// every violation found (INV-SQT-01 leakage is always fatal).
func VerifyStructuralQueryText(structuralZipPath, controlZipPath, engine string) error {
	var errs []string
	digestID := digestCollectorID(engine)

	// --- control: opt-in OFF -> DIGEST_TEXT null, metadata flag false --------
	ctrl, err := ReadQualExport(controlZipPath)
	if err != nil {
		return fmt.Errorf("read control export: %w", err)
	}
	if ctrl.StructuralQueryTextEnabled {
		errs = append(errs, "control export metadata collect_normalized_query_text=true (expected false)")
	}
	for i, row := range ctrl.PayloadByQuery[digestID] {
		if v := row["DIGEST_TEXT"]; v != nil {
			errs = append(errs, fmt.Sprintf("control %s row %d: DIGEST_TEXT not null with opt-in off: %v", digestID, i, v))
			break
		}
	}

	// --- structural: opt-in ON ----------------------------------------------
	st, err := ReadQualExport(structuralZipPath)
	if err != nil {
		return fmt.Errorf("read structural export: %w", err)
	}
	if !st.StructuralQueryTextEnabled {
		errs = append(errs, "structural export metadata collect_normalized_query_text=false (expected true)")
	}

	rows := st.PayloadByQuery[digestID]
	if len(rows) == 0 {
		// No digest evidence => cannot positively prove INV-SQT-01. Fail closed
		// (SQT-R009): the line is not qualified. (For MariaDB this usually means
		// performance_schema / the statements-digest consumer is off.)
		errs = append(errs, fmt.Sprintf("structural export has no %s rows — cannot prove INV-SQT-01; capability stays unsupported for %s (fail closed, SQT-R009)", digestID, engine))
		return fmt.Errorf("structural-query qualification FAILED for %s:\n  - %s", engine, strings.Join(errs, "\n  - "))
	}

	var (
		nonNull        int
		sawPlaceholder bool
		sawTruncated   bool
		maxLen         int
	)
	for i, row := range rows {
		v, ok := row["DIGEST_TEXT"]
		if !ok || v == nil {
			continue
		}
		s, isStr := v.(string)
		if !isStr {
			errs = append(errs, fmt.Sprintf("%s row %d: DIGEST_TEXT is not a string: %T", digestID, i, v))
			continue
		}
		nonNull++
		if len(s) > maxLen {
			maxLen = len(s)
		}
		if strings.Contains(s, "?") {
			sawPlaceholder = true
		}
		// INV-SQT-01: no runtime literal may appear. Always fatal.
		for _, lit := range structuralAdversarialLiterals {
			if strings.Contains(s, lit) {
				errs = append(errs, fmt.Sprintf("INV-SQT-01 VIOLATION — %s row %d exposes runtime literal %q in DIGEST_TEXT: %q", digestID, i, lit, s))
			}
		}
		// Bound + truncation contract (SQT-R003).
		if len(s) > singlestore.StructuralQueryTextMaxBytes {
			errs = append(errs, fmt.Sprintf("%s row %d: DIGEST_TEXT %d bytes exceeds cap %d", digestID, i, len(s), singlestore.StructuralQueryTextMaxBytes))
		}
		if !utf8.ValidString(s) {
			errs = append(errs, fmt.Sprintf("%s row %d: DIGEST_TEXT is not valid UTF-8", digestID, i))
		}
		truncated, _ := row[singlestore.DigestTextTruncatedField].(bool)
		if truncated {
			sawTruncated = true
			if !strings.HasSuffix(s, singlestore.StructuralQueryTextMarker) {
				errs = append(errs, fmt.Sprintf("%s row %d: digest_text_truncated=true but no truncation marker", digestID, i))
			}
		} else if strings.HasSuffix(s, singlestore.StructuralQueryTextMarker) {
			errs = append(errs, fmt.Sprintf("%s row %d: carries truncation marker but digest_text_truncated=false (marker must not substitute for the flag)", digestID, i))
		}
	}

	if nonNull == 0 {
		errs = append(errs, fmt.Sprintf("%s: opt-in on but every DIGEST_TEXT is null — no structural evidence surfaced", digestID))
	}
	if !sawPlaceholder {
		errs = append(errs, fmt.Sprintf("%s: no DIGEST_TEXT contains a `?` placeholder — normalization not observed", digestID))
	}
	// Truncation is OPPORTUNISTIC, not a hard gate. The server caps DIGEST_TEXT
	// at performance_schema_max_digest_length (default 1024 bytes) BEFORE Signals
	// reads it, so a server-produced value only exceeds Signals' 4096-byte cap
	// when that server variable is raised. The truncation LOGIC is proven by the
	// unit test (TC-SQT-05); here we assert the live contract only when a value
	// actually exceeded the cap, and always record the max observed length (the
	// measurement the cap decision will use). maxObserved <= cap is a legitimate
	// outcome (the server's own digest-length cap is below Signals'), never a
	// failure.
	if maxLen > singlestore.StructuralQueryTextMaxBytes && !sawTruncated {
		errs = append(errs, fmt.Sprintf("%s: a DIGEST_TEXT exceeded %d bytes but no row was flagged digest_text_truncated — truncation contract broken", digestID, singlestore.StructuralQueryTextMaxBytes))
	}

	// SQT-R002 / INV-SQT-04: under the structural opt-in (generic
	// high-sensitivity OFF), EVERY other collector's declared SensitiveColumns
	// must stay redacted. Derive the set from the engine's real catalog rather
	// than hardcoding — MySQL and MariaDB name these columns/collectors
	// differently (e.g. mysql_data_locks_v1/LOCK_DATA vs
	// mariadb_innodb_locks_v1/lock_data), and assuming MySQL names would miss a
	// MariaDB leak.
	for _, c := range singlestore.CatalogFor(engine) {
		if c.ID == digestID || len(c.SensitiveColumns) == 0 {
			continue
		}
		for _, col := range c.SensitiveColumns {
			for i, row := range st.PayloadByQuery[c.ID] {
				if v, ok := row[col]; ok && v != nil {
					errs = append(errs, fmt.Sprintf("SQT-R002 VIOLATION — %s row %d exposes sensitive column %s under the structural opt-in: %v", c.ID, i, col, v))
					break
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("structural-query qualification FAILED for %s:\n  - %s", engine, strings.Join(errs, "\n  - "))
	}
	return nil
}

// StructuralQualMaxObservedDigestBytes is a reporting helper: the largest
// server-produced DIGEST_TEXT byte length seen in the structural export, before
// Signals' cap. Informational only (the user wants the measurement recorded to
// inform a later cap decision); never gates qualification.
func StructuralQualMaxObservedDigestBytes(structuralZipPath, engine string) (int, error) {
	st, err := ReadQualExport(structuralZipPath)
	if err != nil {
		return 0, err
	}
	max := 0
	for _, row := range st.PayloadByQuery[digestCollectorID(engine)] {
		if s, ok := row["DIGEST_TEXT"].(string); ok && len(s) > max {
			max = len(s)
		}
	}
	return max, nil
}
