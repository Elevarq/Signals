// Package doctor implements the read-only operator pre-flight checks
// surfaced via `signalsctl doctor` (R095). The package separates pure
// check logic from the CLI shell so individual checks can be unit
// tested without spinning up a CLI command tree.
//
// Spec:        specifications/doctor.md
// Acceptance:  specifications/doctor.acceptance.md
package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/elevarq/arq-signals/internal/collector"
	"github.com/elevarq/arq-signals/internal/config"
	"github.com/elevarq/arq-signals/internal/db"
	"github.com/elevarq/arq-signals/internal/pgqueries"
)

// perTargetParallelism caps the number of targets that can be checked
// in parallel by Run. Each target opens at most one TCP connection
// (C3) and one pgx pool (C4) so the cost is bounded; 8 is a safe
// default that still cuts wall-clock by ~8x for fleets with many
// firewalled / slow targets.
const perTargetParallelism = 8

// Status is the per-check outcome enum. Lower-case values match the
// JSON contract in the spec.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// CheckResult is a single check's outcome. Target is empty when the
// check is not per-target (C1, C2).
type CheckResult struct {
	ID       string
	Name     string
	Target   string
	Status   Status
	Detail   string
	Duration time.Duration
}

// MarshalJSON honours the spec's wire shape: duration_ms instead of
// the runtime time.Duration value.
func (r CheckResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Target     string `json:"target"`
		Status     Status `json:"status"`
		Detail     string `json:"detail"`
		DurationMS int64  `json:"duration_ms"`
	}{
		ID:         r.ID,
		Name:       r.Name,
		Target:     r.Target,
		Status:     r.Status,
		Detail:     r.Detail,
		DurationMS: r.Duration.Milliseconds(),
	})
}

// Summary mirrors the JSON contract in the spec.
type Summary struct {
	OK   int `json:"ok"`
	Warn int `json:"warn"`
	Fail int `json:"fail"`
}

// Report is the doctor's full output for a run.
type Report struct {
	SchemaVersion string        `json:"schema_version"`
	GeneratedAt   string        `json:"generated_at"`
	Checks        []CheckResult `json:"checks"`
	Summary       Summary       `json:"summary"`
}

// SchemaVersion is the wire-protocol version surfaced via JSON output.
const SchemaVersion = "1"

// SupportedCheckIDs lists every check ID the doctor knows about. The
// CLI uses this for `--check` validation (FC-DOC-05 / FC-13).
var SupportedCheckIDs = []string{"C1", "C2", "C3", "C4", "C5", "C6"}

const targetReachableTimeout = 3 * time.Second

// CheckConfigValid (C1) returns OK when the config file exists,
// parses, and passes ValidateStrict. FAIL otherwise.
func CheckConfigValid(configPath string) CheckResult {
	start := time.Now()
	result := CheckResult{ID: "C1", Name: "config_valid"}

	if _, statErr := os.Stat(configPath); statErr != nil {
		result.Status = StatusFail
		result.Detail = fmt.Sprintf("config file not accessible at %s: %v", configPath, statErr)
		result.Duration = time.Since(start)
		return result
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		result.Status = StatusFail
		result.Detail = fmt.Sprintf("load %s: %v", configPath, err)
		result.Duration = time.Since(start)
		return result
	}

	if _, err := config.ValidateStrict(cfg); err != nil {
		result.Status = StatusFail
		result.Detail = fmt.Sprintf("ValidateStrict on %s: %v", configPath, err)
		result.Duration = time.Since(start)
		return result
	}

	result.Status = StatusOK
	result.Detail = "loaded " + configPath
	result.Duration = time.Since(start)
	return result
}

// CheckStoreWritable (C2) returns OK when the configured SQLite store
// directory exists and is writable. INV-DOC-02: the probe file is
// removed immediately after the write.
func CheckStoreWritable(storePath string) CheckResult {
	start := time.Now()
	result := CheckResult{ID: "C2", Name: "store_writable"}

	// The store path may be a file (e.g. "/var/lib/arq-signals.db")
	// rather than a directory. Probe the parent directory, since the
	// daemon creates the file there. Two cases promote to the parent:
	//   1. Path exists as a regular file (daemon ran before).
	//   2. Path does NOT exist and the basename looks file-like
	//      (has an extension) — typical for the default
	//      /data/arq-signals.db. Without this, an operator pointing
	//      at a missing-parent path would see "write probe in
	//      /data/arq-signals.db: ENOENT" instead of the actually
	//      missing directory /data.
	dir := storePath
	info, statErr := os.Stat(storePath)
	switch {
	case statErr == nil && !info.IsDir():
		dir = filepath.Dir(storePath)
	case statErr != nil && os.IsNotExist(statErr) && filepath.Ext(storePath) != "":
		dir = filepath.Dir(storePath)
	}

	probe, err := os.CreateTemp(dir, ".signalsctl-doctor-probe-*")
	if err != nil {
		result.Status = StatusFail
		result.Detail = fmt.Sprintf("write probe in %s: %v", dir, err)
		result.Duration = time.Since(start)
		return result
	}
	probePath := probe.Name()
	_ = probe.Close()
	if removeErr := os.Remove(probePath); removeErr != nil {
		result.Status = StatusFail
		result.Detail = fmt.Sprintf("remove probe %s: %v (INV-DOC-02 broken)", probePath, removeErr)
		result.Duration = time.Since(start)
		return result
	}

	result.Status = StatusOK
	result.Detail = dir
	result.Duration = time.Since(start)
	return result
}

// CheckTargetReachable (C3) attempts a TCP dial against the target's
// configured host:port with a tight timeout. WARN when the target is
// disabled; FAIL on dial error; OK on successful dial.
func CheckTargetReachable(ctx context.Context, tgt config.TargetConfig) CheckResult {
	start := time.Now()
	result := CheckResult{ID: "C3", Name: "target_reachable", Target: tgt.Name}

	if !tgt.Enabled {
		result.Status = StatusWarn
		result.Detail = "target is disabled in config"
		result.Duration = time.Since(start)
		return result
	}

	addr := net.JoinHostPort(tgt.Host, strconv.Itoa(tgt.Port))
	dialer := &net.Dialer{Timeout: targetReachableTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		result.Status = StatusFail
		result.Detail = fmt.Sprintf("dial %s: %v", addr, err)
		result.Duration = time.Since(start)
		return result
	}
	_ = conn.Close()
	result.Status = StatusOK
	result.Detail = fmt.Sprintf("reached %s in %dms", addr, time.Since(start).Milliseconds())
	result.Duration = time.Since(start)
	return result
}

// CheckRoleSafe (C4) opens a read-only connection and validates the
// configured role does not hold superuser / replication / bypassrls
// (mirrors collector.ValidateRoleSafety). Emits WARN when the upstream
// target_reachable failed (INV-DOC-04).
func CheckRoleSafe(ctx context.Context, tgt config.TargetConfig, reachable bool) CheckResult {
	start := time.Now()
	result := CheckResult{ID: "C4", Name: "role_safe", Target: tgt.Name}

	if !reachable {
		result.Status = StatusWarn
		result.Detail = "skipped (target_reachable failed)"
		result.Duration = time.Since(start)
		return result
	}

	dsn, err := collector.BuildSafeDSN(tgt)
	if err != nil {
		// Password resolution failed (missing password_env, unreadable
		// password_file, etc.). Surface it as the C4 failure instead
		// of letting an empty-password connection produce a misleading
		// "authentication failed" error downstream.
		result.Status = StatusFail
		result.Detail = "resolve password: " + collector.RedactError(err).Error()
		result.Duration = time.Since(start)
		return result
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		// Redact in case pgxpool ever echoes the DSN.
		result.Status = StatusFail
		result.Detail = fmt.Sprintf("connect: %s", collector.RedactDSN(err.Error()))
		result.Duration = time.Since(start)
		return result
	}
	defer pool.Close()

	safety, err := collector.ValidateRoleSafety(ctx, pool)
	if err != nil {
		result.Status = StatusFail
		result.Detail = "role check: " + collector.RedactError(err).Error()
		result.Duration = time.Since(start)
		return result
	}
	if !safety.IsSafe() {
		result.Status = StatusFail
		result.Detail = safety.Error()
		result.Duration = time.Since(start)
		return result
	}

	result.Status = StatusOK
	result.Detail = fmt.Sprintf("role %s has no unsafe attributes", tgt.User)
	result.Duration = time.Since(start)
	return result
}

// buildSafeDSN moved to collector.BuildSafeDSN (R096 shared helper).
// Kept as a deprecation marker so future code searches for the old
// name find the new location.

// CheckCollectorPrerequisites (C5) classifies each enabled collector
// against the target's actual PG version + installed extensions.
// Depends on C3 + C4 — emits WARN ("skipped (target_reachable /
// role_safe failed)") when the upstream connection isn't available
// (FC-DOC-06). FAIL only when every registered collector would be
// gated (catastrophic mismatch — e.g. PG 9.6 against our PG 14+
// catalog). Otherwise OK (no missing extensions / version gates) or
// WARN (some collectors gated; partial coverage).
func CheckCollectorPrerequisites(ctx context.Context, tgt config.TargetConfig, reachable bool, highSensitivityEnabled bool) CheckResult {
	start := time.Now()
	result := CheckResult{ID: "C5", Name: "collector_prerequisites", Target: tgt.Name}

	if !reachable {
		result.Status = StatusWarn
		result.Detail = "skipped (target_reachable / role_safe failed)"
		result.Duration = time.Since(start)
		return result
	}

	dsn, err := collector.BuildSafeDSN(tgt)
	if err != nil {
		result.Status = StatusWarn
		result.Detail = "skipped (resolve password: " + collector.RedactError(err).Error() + ")"
		result.Duration = time.Since(start)
		return result
	}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(dialCtx, dsn)
	if err != nil {
		result.Status = StatusWarn
		result.Detail = "skipped (connect: " + collector.RedactDSN(err.Error()) + ")"
		result.Duration = time.Since(start)
		return result
	}
	defer pool.Close()

	probe, err := runDiscoveryProbe(dialCtx, pool)
	if err != nil {
		result.Status = StatusWarn
		result.Detail = "discovery probe failed: " + collector.RedactError(err).Error()
		result.Duration = time.Since(start)
		return result
	}

	gated := pgqueries.GatedIDsByReason(pgqueries.FilterParams{
		PGMajorVersion:         probe.MajorVersion,
		Extensions:             probe.Extensions,
		ExtensionVersions:      probe.ExtensionVersions, // R115
		HighSensitivityEnabled: highSensitivityEnabled,
	})
	totalRegistered := len(pgqueries.All())
	totalGated := 0
	for _, ids := range gated {
		totalGated += len(ids)
	}
	available := totalRegistered - totalGated

	// Format the detail: always show the available count; name each
	// gated reason with its first few collector IDs (full list would
	// flood the terminal for missing core extensions).
	parts := []string{fmt.Sprintf("%d available", available)}
	for _, reason := range []string{
		pgqueries.GateReasonVersionUnsupported,
		pgqueries.GateReasonExtensionMissing,
		pgqueries.GateReasonConfigDisabled,
	} {
		if ids, ok := gated[reason]; ok && len(ids) > 0 {
			parts = append(parts, fmt.Sprintf("%d %s (%s)", len(ids), reason, summariseIDs(ids)))
		}
	}
	result.Detail = strings.Join(parts, ", ")

	switch {
	case available == 0:
		result.Status = StatusFail
	case len(gated[pgqueries.GateReasonExtensionMissing]) > 0 || len(gated[pgqueries.GateReasonVersionUnsupported]) > 0:
		result.Status = StatusWarn
	default:
		// config_disabled is intentional operator state, not a problem.
		result.Status = StatusOK
	}

	result.Duration = time.Since(start)
	return result
}

// runDiscoveryProbe opens a read-only transaction and runs the
// catalog's Discover helper. Doctor doesn't share the daemon's
// transaction infrastructure so we wire one up locally.
func runDiscoveryProbe(ctx context.Context, pool *pgxpool.Pool) (pgqueries.Discovery, error) {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return pgqueries.Discovery{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	return pgqueries.Discover(ctx, tx)
}

// summariseIDs returns a short comma-separated representation of an
// ID list, truncated for readability. Drives C5's Detail formatting.
func summariseIDs(ids []string) string {
	const maxItems = 3
	if len(ids) <= maxItems {
		return strings.Join(ids, ", ")
	}
	return strings.Join(ids[:maxItems], ", ") + fmt.Sprintf(", +%d more", len(ids)-maxItems)
}

// CheckSnapshotFreshness (C6) reads the daemon's local SQLite store
// and reports the age of the most recent completed snapshot for the
// target against the configured poll_interval.
//
// FC-DOC-07: a missing / unreadable store is WARN, not FAIL — pre-
// daemon doctor runs are a valid use case. Same for "store query
// failed" (transient SQLite issue).
//
// FAIL only when the store IS readable and confirms zero snapshots
// for an enabled target — the daemon should have collected at least
// one by now and hasn't.
func CheckSnapshotFreshness(storePath string, tgt config.TargetConfig, pollInterval time.Duration) CheckResult {
	start := time.Now()
	result := CheckResult{ID: "C6", Name: "snapshot_freshness", Target: tgt.Name}

	// sqlite's database/sql driver is lazy — sql.Open and db.Open
	// only validate args. The real "is this file readable?" check
	// only fires on the first query. Stat the path up front so a
	// non-existent / unreadable store surfaces as a recognisable
	// store_unreadable warning instead of a deeper SQLite error
	// string that varies by driver build.
	if _, err := os.Stat(storePath); err != nil {
		result.Status = StatusWarn
		result.Detail = "store unreadable: " + err.Error()
		result.Duration = time.Since(start)
		return result
	}

	store, err := db.Open(storePath, false)
	if err != nil {
		result.Status = StatusWarn
		result.Detail = "store unreadable: " + err.Error()
		result.Duration = time.Since(start)
		return result
	}
	defer func() { _ = store.Close() }()

	last, found, err := store.GetLatestSnapshotTimeByTargetName(tgt.Name)
	if err != nil {
		result.Status = StatusWarn
		result.Detail = "store query failed: " + err.Error()
		result.Duration = time.Since(start)
		return result
	}
	if !found {
		result.Status = StatusFail
		result.Detail = "no completed snapshots"
		result.Duration = time.Since(start)
		return result
	}

	age := time.Since(last).Truncate(time.Second)
	threshold := 2 * pollInterval
	if age > threshold {
		result.Status = StatusWarn
		result.Detail = fmt.Sprintf("%v ago, poll_interval=%v, exceeded 2x threshold", age, pollInterval)
	} else {
		result.Status = StatusOK
		result.Detail = fmt.Sprintf("%v ago, poll_interval=%v", age, pollInterval)
	}
	result.Duration = time.Since(start)
	return result
}

// Run runs the requested checks against the given config and returns
// the union of findings. An empty selectedIDs slice runs every
// supported check. Returns an error for usage problems (unknown
// check ID) that the caller should surface with exit code 2.
func Run(ctx context.Context, configPath string, selectedIDs []string) (Report, error) {
	selected, err := normalizeCheckSelection(selectedIDs)
	if err != nil {
		return Report{}, err
	}

	report := Report{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	// C1: config_valid.
	var cfg config.Config
	var cfgLoaded bool
	if selected["C1"] {
		c1 := CheckConfigValid(configPath)
		report.Checks = append(report.Checks, c1)
		if c1.Status == StatusOK {
			cfg, _ = config.Load(configPath)
			cfgLoaded = true
		}
	}

	// C2: store_writable. Depends on having a loadable config so we
	// know which path to probe. When the config didn't load, we still
	// run C2 if requested — using the path-string proxy below — so an
	// operator running doctor before fixing config can still see the
	// store-path issue independently.
	if selected["C2"] {
		// Even if C1 hasn't been requested or failed, attempt a load.
		// If load fails the C2 check uses the default config's path,
		// which is correct: doctor probes what the daemon would use.
		if !cfgLoaded {
			loaded, loadErr := config.Load(configPath)
			if loadErr == nil {
				cfg = loaded
				cfgLoaded = true
			} else {
				cfg = config.DefaultConfig()
			}
		}
		report.Checks = append(report.Checks, CheckStoreWritable(cfg.Database.Path))
	}

	// Per-target checks need a config. If C1 failed we still produce
	// per-target placeholders so the report is honest about what was
	// requested vs what could be evaluated.
	if selected["C3"] || selected["C4"] || selected["C5"] || selected["C6"] {
		if !cfgLoaded {
			loaded, loadErr := config.Load(configPath)
			if loadErr == nil {
				cfg = loaded
				cfgLoaded = true
			}
		}

		if cfgLoaded {
			// Per-target work runs in parallel with a small concurrency
			// cap. Results land in a per-slot buffer so the final report
			// is deterministic regardless of completion order.
			//
			// normalizeCheckSelection guarantees C3 is selected
			// whenever C4 is, so c3 is always populated before c4
			// runs within a slot. C5 also depends on a reachable +
			// safe target, so it consults c3 and c4's outcomes. C6
			// reads the daemon's SQLite store and is independent of
			// the per-target connection — it can succeed even when
			// C3/C4 fail.
			type targetSlot struct {
				c3, c4, c5, c6 *CheckResult
			}
			slots := make([]targetSlot, len(cfg.Targets))

			eg, egCtx := errgroup.WithContext(ctx)
			eg.SetLimit(perTargetParallelism)

			for i, tgt := range cfg.Targets {
				i, tgt := i, tgt
				eg.Go(func() error {
					var slot targetSlot
					if selected["C3"] {
						r := CheckTargetReachable(egCtx, tgt)
						slot.c3 = &r
					}
					reachable := slot.c3 != nil && slot.c3.Status == StatusOK
					if selected["C4"] {
						r := CheckRoleSafe(egCtx, tgt, reachable)
						slot.c4 = &r
					}
					if selected["C5"] {
						// C5 needs both reachable AND role-safe; if
						// either upstream failed, surface WARN with
						// the dependency reason.
						upstreamOK := reachable
						if slot.c4 != nil && slot.c4.Status != StatusOK {
							upstreamOK = false
						}
						r := CheckCollectorPrerequisites(egCtx, tgt, upstreamOK, cfg.Signals.HighSensitivityCollectorsEnabled)
						slot.c5 = &r
					}
					if selected["C6"] {
						r := CheckSnapshotFreshness(cfg.Database.Path, tgt, cfg.Signals.PollInterval)
						slot.c6 = &r
					}
					slots[i] = slot
					return nil
				})
			}
			_ = eg.Wait()

			for _, slot := range slots {
				if slot.c3 != nil {
					report.Checks = append(report.Checks, *slot.c3)
				}
				if slot.c4 != nil {
					report.Checks = append(report.Checks, *slot.c4)
				}
				if slot.c5 != nil {
					report.Checks = append(report.Checks, *slot.c5)
				}
				if slot.c6 != nil {
					report.Checks = append(report.Checks, *slot.c6)
				}
			}
		}
	}

	report.Summary = summarize(report.Checks)
	return report, nil
}

func normalizeCheckSelection(selectedIDs []string) (map[string]bool, error) {
	supported := make(map[string]bool, len(SupportedCheckIDs))
	for _, id := range SupportedCheckIDs {
		supported[id] = true
	}

	if len(selectedIDs) == 0 {
		return supported, nil
	}

	out := map[string]bool{}
	var unknown []string
	for _, id := range selectedIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if !supported[id] {
			unknown = append(unknown, id)
			continue
		}
		out[id] = true
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf(
			"unknown check id(s): %s (supported: %s)",
			strings.Join(unknown, ", "),
			strings.Join(SupportedCheckIDs, ", "),
		)
	}
	// C4 depends on C3 (role_safe needs target_reachable to gate the
	// dial attempt). Auto-add C3 when C4 is selected so the operator
	// doesn't see an entire fleet of WARN rows with a confusing
	// "skipped (target_reachable failed)" detail just because they
	// forgot the dependency. C3 is cheap (TCP dial with a 3s cap).
	if out["C4"] && !out["C3"] {
		out["C3"] = true
	}
	// C5 needs both reachable AND role_safe (it opens a pool and
	// queries pg_extension). Auto-add both. Cascades through C4's
	// own auto-add above (we only need to set C4 here).
	if out["C5"] && !out["C4"] {
		out["C4"] = true
		out["C3"] = true
	}
	// C6 is independent — reads the daemon's SQLite store, not the
	// target. No dependencies to auto-add.
	return out, nil
}

func summarize(checks []CheckResult) Summary {
	var s Summary
	for _, c := range checks {
		switch c.Status {
		case StatusOK:
			s.OK++
		case StatusWarn:
			s.Warn++
		case StatusFail:
			s.Fail++
		}
	}
	return s
}
