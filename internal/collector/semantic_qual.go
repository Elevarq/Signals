package collector

// Semantic collector qualification (specifications/semantic-collector-qualification.md).
//
// This layer makes a semantic-value assertion a REQUIRED step in the
// collector-qualification lifecycle. The governing principle is "collector
// success != collector qualification": a collector that merely executed
// (status='success'), whose shape reconciled (VerifyCadenceSnapshot /
// VerifyExportReconciliation), or whose rows looked valid is NOT qualified — it
// may still emit semantically wrong values. Qualification requires that the
// emitted VALUES match a contract against controlled database state.
//
// It is additive (SCQ-INV-2): it reuses the engine-specific semantic verifier
// that already exists (MySQL/MariaDB Replay Tier-1, VerifyReplayTier1Evidence)
// and names the smallest generic verifier (VerifySemanticContract) for engines
// that previously had only status/shape checks. Nothing here changes the
// existing qualification layers or PostgreSQL behaviour.

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/elevarq/signals/internal/config"
)

// CollectorQualLevel is the qualification level a collector has reached. The
// levels are strictly ordered and MUST NOT be conflated (SCQ-INV-1): executed
// and structurally_validated say nothing about value correctness; only
// semantically_qualified may be reported as "semantic contract validated".
type CollectorQualLevel int

const (
	// QualExecuted: the collector ran and returned status='success'.
	QualExecuted CollectorQualLevel = iota
	// QualStructurallyValidated: output shape + row-count reconciliation held.
	QualStructurallyValidated
	// QualSemanticallyQualified: emitted values matched a contract against
	// controlled database state — the only level that proves correctness.
	QualSemanticallyQualified
)

// String renders the level for qualification reports, keeping the "collector
// executed" vs "semantic contract validated" distinction explicit (SCQ-R007).
func (l CollectorQualLevel) String() string {
	switch l {
	case QualExecuted:
		return "executed"
	case QualStructurallyValidated:
		return "structurally_validated"
	case QualSemanticallyQualified:
		return "semantically_qualified"
	default:
		return "unknown"
	}
}

// FieldKind is the semantic kind a critical field's cell must satisfy.
type FieldKind int

const (
	// KindText: any non-empty string (free-form value).
	KindText FieldKind = iota
	// KindIdentifier: a non-empty, whitespace-clean token (schema/table/name).
	KindIdentifier
	// KindNumeric: a value that parses as a number (counter, size, count).
	KindNumeric
	// KindTimestamp: a non-empty time value (presence only; format is engine-native).
	KindTimestamp
)

// FieldContract declares one critical field a semantically-qualified collector
// must emit: its semantic kind, physical unit (for the human inventory), and
// whether every row must carry a non-empty value for it (SCQ-R001).
type FieldContract struct {
	Name     string
	Kind     FieldKind
	Unit     string // e.g. "bytes", "rows", "count"; "" for identifiers/dimensionless
	Required bool
}

// SemanticContract is the checked-in, per-collector semantic inventory
// (SCQ-R001): the critical fields + kinds + units + zero-row legitimacy a
// qualified collector's output must satisfy. For collectors with a richer
// dedicated verifier (MySQL/MariaDB Replay Tier-1), Verifier is set and takes
// precedence; Fields then documents the human-facing inventory.
type SemanticContract struct {
	CollectorID  string
	Engine       string
	Level        CollectorQualLevel
	Fields       []FieldContract
	ZeroRowLegit bool   // true: an empty result is a legitimate semantic answer
	EvidenceNote string // the controlled state this is qualified against
	Verifier     func(QualExport) error
}

// EffectiveVerifier returns the value-assertion for a contract: the dedicated
// Verifier if set, else a generic verifier built from Fields. It returns nil
// when a contract has NEITHER — a semantically_qualified contract in that state
// has no teeth and is a lifecycle violation the gate catches (SCQ-R003).
func (c SemanticContract) EffectiveVerifier() func(QualExport) error {
	if c.Verifier != nil {
		return c.Verifier
	}
	if len(c.Fields) == 0 {
		return nil
	}
	cc := c
	return func(z QualExport) error { return VerifySemanticContract(z, cc) }
}

// VerifySemanticContract asserts the collector's exported rows satisfy the field
// contract against actual values, not merely that the collector executed
// (SCQ-R002). It returns an error listing every violation, in row-then-field
// order for determinism (SCQ-INV-3), or nil when the contract holds.
func VerifySemanticContract(z QualExport, c SemanticContract) error {
	rows := z.PayloadByQuery[c.CollectorID]
	var errs []string

	if len(rows) == 0 {
		if !c.ZeroRowLegit {
			errs = append(errs, fmt.Sprintf("%s: no rows (a zero-row result is not a legitimate semantic answer for this collector)", c.CollectorID))
			return fmt.Errorf("semantic contract failed for %s (%s): %s", c.CollectorID, c.Engine, strings.Join(errs, "; "))
		}
		return nil // zero rows legitimate ⇒ vacuously satisfied
	}

	for i, r := range rows {
		for _, f := range c.Fields {
			raw, present := r[f.Name]
			if !present || raw == nil {
				if f.Required {
					errs = append(errs, fmt.Sprintf("%s row %d: required field %q absent", c.CollectorID, i, f.Name))
				}
				continue
			}
			s := cell(r, f.Name)
			if strings.TrimSpace(s) == "" {
				if f.Required {
					errs = append(errs, fmt.Sprintf("%s row %d: required field %q empty", c.CollectorID, i, f.Name))
				}
				continue
			}
			switch f.Kind {
			case KindNumeric:
				if _, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err != nil {
					unit := f.Unit
					if unit == "" {
						unit = "dimensionless"
					}
					errs = append(errs, fmt.Sprintf("%s row %d: field %q=%q is not numeric (expected %s)", c.CollectorID, i, f.Name, s, unit))
				}
			case KindIdentifier:
				if s != strings.TrimSpace(s) {
					errs = append(errs, fmt.Sprintf("%s row %d: identifier field %q=%q has surrounding whitespace", c.CollectorID, i, f.Name, s))
				}
			case KindText, KindTimestamp:
				// presence + non-emptiness already asserted above.
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("semantic contract failed for %s (%s): %s", c.CollectorID, c.Engine, strings.Join(errs, "; "))
	}
	return nil
}

// SemanticQualRegistry is the checked-in authority mapping each engine to its
// semantically-qualified collectors (SCQ-R001). It is REPRESENTATIVE for the POC
// (SCQ-R004): one qualified collector per engine, proving the lifecycle end to
// end. Exhaustive per-collector coverage is the 2.0 release-blocker scope
// (Elevarq/Signals#624), deliberately not populated here (SCQ scope note).
//
// MySQL and MariaDB reuse the rich Replay Tier-1 verifier (column order, type,
// nullability, composite-index ordering); SingleStore, ClickHouse and
// PostgreSQL use the generic field/kind/unit contract. PostgreSQL's full
// producer regression continues to live in the PostgreSQL regression fixtures
// (SCQ-R006); its entry here is the representative POC assertion.
func SemanticQualRegistry() map[string][]SemanticContract {
	return map[string][]SemanticContract{
		config.EnginePostgreSQL: {{
			CollectorID: "bloat_estimate_v1",
			Engine:      config.EnginePostgreSQL,
			Level:       QualSemanticallyQualified,
			Fields: []FieldContract{
				{Name: "schemaname", Kind: KindIdentifier, Required: true},
				{Name: "tablename", Kind: KindIdentifier, Required: true},
				{Name: "n_live_tup", Kind: KindNumeric, Unit: "rows", Required: true},
			},
			EvidenceNote: "seeded relation with a known live-tuple count; full PostgreSQL producer regression in the PG regression fixtures (SCQ-R006)",
		}},
		config.EngineSingleStore: {{
			CollectorID: "singlestore_table_statistics_v1",
			Engine:      config.EngineSingleStore,
			Level:       QualSemanticallyQualified,
			Fields: []FieldContract{
				{Name: "DATABASE_NAME", Kind: KindIdentifier, Required: true},
				{Name: "TABLE_NAME", Kind: KindIdentifier, Required: true},
				{Name: "ROWS", Kind: KindNumeric, Unit: "rows", Required: true},
				{Name: "MEMORY_USE", Kind: KindNumeric, Unit: "bytes", Required: true},
			},
			EvidenceNote: "seeded table in signals_fixture with a known row count (information_schema.TABLE_STATISTICS)",
		}},
		config.EngineMySQL: {{
			CollectorID: "mysql_columns_v1",
			Engine:      config.EngineMySQL,
			Level:       QualSemanticallyQualified,
			Fields: []FieldContract{
				{Name: "COLUMN_NAME", Kind: KindIdentifier, Required: true},
				{Name: "ORDINAL_POSITION", Kind: KindNumeric, Required: true},
				{Name: "COLUMN_TYPE", Kind: KindText, Required: true},
				{Name: "IS_NULLABLE", Kind: KindText, Required: true},
			},
			EvidenceNote: "Replay Tier-1 column + index inventory against signals_fixture.replay_fixture (#538)",
			Verifier:     func(z QualExport) error { return VerifyReplayTier1Evidence(z, config.EngineMySQL) },
		}},
		config.EngineMariaDB: {{
			CollectorID: "mariadb_columns_v1",
			Engine:      config.EngineMariaDB,
			Level:       QualSemanticallyQualified,
			Fields: []FieldContract{
				{Name: "COLUMN_NAME", Kind: KindIdentifier, Required: true},
				{Name: "ORDINAL_POSITION", Kind: KindNumeric, Required: true},
				{Name: "COLUMN_TYPE", Kind: KindText, Required: true},
				{Name: "IS_NULLABLE", Kind: KindText, Required: true},
			},
			EvidenceNote: "Replay Tier-1 column + index inventory against signals_fixture.replay_fixture (#538)",
			Verifier:     func(z QualExport) error { return VerifyReplayTier1Evidence(z, config.EngineMariaDB) },
		}},
		config.EngineClickHouse: {{
			CollectorID: "clickhouse_parts_v1",
			Engine:      config.EngineClickHouse,
			Level:       QualSemanticallyQualified,
			Fields: []FieldContract{
				{Name: "database", Kind: KindIdentifier, Required: true},
				{Name: "table", Kind: KindIdentifier, Required: true},
				{Name: "parts", Kind: KindNumeric, Unit: "count", Required: true},
				{Name: "rows", Kind: KindNumeric, Unit: "rows", Required: true},
				{Name: "bytes_on_disk", Kind: KindNumeric, Unit: "bytes", Required: true},
			},
			EvidenceNote: "seeded MergeTree table with a known part/row count (system.parts aggregate, #562)",
		}},
	}
}

// SemanticQualReportLine renders one registry entry for a qualification report,
// keeping the level explicit (SCQ-R007). Deterministic, single-line.
func SemanticQualReportLine(c SemanticContract) string {
	return fmt.Sprintf("%s/%s: %s", c.Engine, c.CollectorID, c.Level)
}
