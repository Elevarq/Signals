package export

import (
	"archive/zip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/elevarq/signals/internal/collector"
	"github.com/elevarq/signals/internal/db"
	"github.com/elevarq/signals/internal/pgqueries"
	"github.com/elevarq/signals/internal/safety"
	"github.com/elevarq/signals/snapshot"
)

// ErrSnapshotNotFound is returned when an export is requested with
// SnapshotID set to an ID that does not exist in the daemon's
// store. Callers (HTTP handler / signalsctl) translate this to FC-08
// (HTTP 404 / non-zero exit). Distinct from a generic SQL error so
// the API layer doesn't have to scrape error strings.
var ErrSnapshotNotFound = errors.New("snapshot not found")

// ErrConflictingSelectors is returned when the caller supplies two
// selectors that name disjoint scopes — currently `All` and
// `SnapshotID` together. Translated to HTTP 400 by the API layer.
var ErrConflictingSelectors = errors.New("conflicting export selectors: --all and --snapshot-id are mutually exclusive")

// Collection-status markers embedded in metadata.json (SIGNALS-R125 /
// INV-SIGNALS-25). They let a consumer machine-detect emptiness or
// failure without inferring it from an absent snapshot row.
const (
	// CollectionStatusOK — successful collection data is packaged.
	CollectionStatusOK = "ok"
	// CollectionStatusNoCollectionYet — no enabled target has ever
	// completed a collection cycle.
	CollectionStatusNoCollectionYet = "no_collection_yet"
	// CollectionStatusLastFailed — an enabled target's most recent
	// cycle failed entirely and no successful data remains.
	CollectionStatusLastFailed = "last_collection_failed"
)

// DefaultScopeCollectionStatus reports the collection_status marker the
// DEFAULT-scope export (R084) would carry, WITHOUT building the ZIP. The
// API layer uses it to decide whether to refuse a data-less default
// export (SIGNALS-R125): "ok" -> serve; "last_collection_failed" /
// "no_collection_yet" -> HTTP 422 with that reason. It also returns the
// bounded failure category (for the last_collection_failed message).
//
//   - "ok"                      — the default scope has at least one
//     successful run to package.
//   - "last_collection_failed"  — no successful default-scope data AND at
//     least one target's last cycle failed. category names the failure.
//   - "no_collection_yet"       — no successful data and no failed cycle
//     recorded (fresh system).
func (b *Builder) DefaultScopeCollectionStatus(targetID int64) (status, category string, err error) {
	runs, err := b.store.GetLatestRunsPerCollector(targetID)
	if err != nil {
		return "", "", fmt.Errorf("latest runs per collector: %w", err)
	}
	for _, r := range runs {
		if r.Status == "success" || r.Status == "" {
			return CollectionStatusOK, "", nil
		}
	}
	return b.emptyScopeStatus()
}

// emptyScopeStatus classifies an export scope that packages no
// successful data: "last_collection_failed" (with the failure category)
// when any target's last cycle failed, else "no_collection_yet".
func (b *Builder) emptyScopeStatus() (status, category string, err error) {
	outcomes, err := b.store.GetCycleOutcomes()
	if err != nil {
		return "", "", fmt.Errorf("cycle outcomes: %w", err)
	}
	for _, o := range outcomes {
		if o.Status == "failed" {
			return CollectionStatusLastFailed, o.Category, nil
		}
	}
	return CollectionStatusNoCollectionYet, "", nil
}

// collectionStatus computes the metadata.json marker for the scope
// actually packaged in THIS ZIP (any scope). "ok" when the packaged
// run set contains a successful run; otherwise the persisted cycle
// outcomes decide last_collection_failed vs no_collection_yet. Errors
// reading cycle outcomes degrade to "no_collection_yet" rather than
// failing the whole export — the marker is advisory metadata, and a
// missing marker would itself be a false-clean risk.
func (b *Builder) collectionStatus(scope *exportScope) string {
	for _, r := range scope.runSet {
		if r.Status == "success" || r.Status == "" {
			return CollectionStatusOK
		}
	}
	status, _, err := b.emptyScopeStatus()
	if err != nil {
		return CollectionStatusNoCollectionYet
	}
	return status
}

// Options controls what data is included in the export.
//
// Selector precedence (R084/R085):
//
//   - SnapshotID set → exactly that snapshot.
//   - All == true OR Since/Until set → all snapshots in scope,
//     optionally filtered by target_id and time range. The
//     pre-R084 behavior, now opt-in.
//   - Otherwise (no selectors) → R084 default: latest completed
//     snapshot per active target, optionally narrowed to a single
//     target by TargetID.
//
// SnapshotID + All in the same call is ErrConflictingSelectors.
type Options struct {
	TargetID   int64
	Since      string
	Until      string
	SnapshotID string // R085: --snapshot-id
	All        bool   // R085: --all
}

// CollectorStatusSchemaVersion is the schema version embedded in
// collector_status.json. Bumped independently of the snapshot schema so
// auditors can pin tooling to a specific status format.
const CollectorStatusSchemaVersion = "1"

// Builder creates a ZIP export of collected data.
//
// R110 concurrency model (issue #323): a Builder holds ONLY set-once
// construction-time configuration (store handle, instance ID,
// unsafe-mode flag, collector-status file, the per-collector-files
// toggle). Every field below is written before the daemon starts
// serving and is read-only for its lifetime, so the single long-lived
// Builder the daemon shares across concurrent GET /export requests is
// safe to call concurrently. Per-request scope is NOT stored here — it
// lives in a per-call exportScope value threaded through the writers,
// so two concurrent WriteTo calls cannot alias each other's scope.
type Builder struct {
	store                            *db.DB
	instanceID                       string
	unsafeMode                       bool
	unsafeReasonsFunc                func() []string
	collectorStatus                  *collector.CollectorStatusFile
	highSensitivityCollectorsEnabled bool
	perCollectorFiles                bool
}

// exportScope is the request-specific scope of a single WriteTo call.
// It is resolved once at the top of WriteTo and passed by value/pointer
// to every writer, so it is local and immutable for that call (issue
// #323). Nothing here is ever stored on the shared Builder.
//
//   - snapshots      — the snapshot rows in scope (R084/R085).
//   - snapshotIDs    — membership set for the scoped snapshots.
//   - runSet         — the resolved query_runs. For selector scopes it
//     is every run in the scoped snapshots; for the R084 default scope
//     it is the latest run per (target_id, query_id). collector_status,
//     query_runs, query_results, and the per-collector view all draw
//     from this one set so they can never disagree (issue #322).
//   - runScope       — metadata.json label (R086): "latest-per-collector"
//     for the default, "snapshot" for selectors.
type exportScope struct {
	snapshots   []db.Snapshot
	snapshotIDs map[string]bool
	runSet      []db.QueryRun
	runScope    string
}

// NewBuilder creates a new export Builder.
func NewBuilder(store *db.DB, instanceID string) *Builder {
	return &Builder{store: store, instanceID: instanceID}
}

// SetCollectorStatus provides collector execution status data for
// inclusion in the export ZIP as collector_status.json.
func (b *Builder) SetCollectorStatus(status *collector.CollectorStatusFile) {
	b.collectorStatus = status
}

// SetUnsafeMode marks the export metadata as collected in unsafe mode.
func (b *Builder) SetUnsafeMode(reasonsFunc func() []string) {
	b.unsafeMode = true
	b.unsafeReasonsFunc = reasonsFunc
}

// SetHighSensitivityCollectorsEnabled records the daemon-wide R075 gate
// state so it can be embedded in export metadata. Auditors use this to
// determine whether application-authored SQL definition text could be
// present in the export without parsing the body.
func (b *Builder) SetHighSensitivityCollectorsEnabled(enabled bool) {
	b.highSensitivityCollectorsEnabled = enabled
}

// SetExportPerCollectorFiles enables R080's per-collector view. When on,
// the export ZIP gains a `per-collector/<query_id>.json` directory with
// the latest run for each collector regrouped from the canonical
// query_runs / query_results data.
func (b *Builder) SetExportPerCollectorFiles(enabled bool) {
	b.perCollectorFiles = enabled
}

// LatestTargetIDs returns the id of every active target that has at least
// one stored snapshot, one id per target. The scheduled auto-exporter
// (#350, SIGNALS-R127) uses it to write one snapshot ZIP per target so a
// downstream consumer receives each database as a separate export — a
// combined multi-target ZIP is read as a single database.
func (b *Builder) LatestTargetIDs() ([]int64, error) {
	snaps, err := b.store.GetLatestSnapshotsPerTarget()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(snaps))
	for _, s := range snaps {
		ids = append(ids, s.TargetID)
	}
	return ids, nil
}

// WriteTo writes the ZIP export to the given writer.
// zipCreator is the subset of *zip.Writer the file writers use. The
// hashingZip wrapper implements it so every entry is hashed as it is
// written (#455).
type zipCreator interface {
	Create(name string) (io.Writer, error)
}

// ManifestName is the ZIP entry carrying the per-file integrity manifest.
const ManifestName = "manifest.json"

// ManifestVersion is the schema version of manifest.json.
const ManifestVersion = 1

// hashingZip wraps a *zip.Writer and tees every entry's bytes through a
// SHA-256 hasher, so a manifest.json listing each file's digest can be
// written last (#455). It records entries in creation order.
type hashingZip struct {
	zw     *zip.Writer
	order  []string
	hashes map[string]hash.Hash
}

func newHashingZip(zw *zip.Writer) *hashingZip {
	return &hashingZip{zw: zw, hashes: map[string]hash.Hash{}}
}

func (h *hashingZip) Create(name string) (io.Writer, error) {
	w, err := h.zw.Create(name)
	if err != nil {
		return nil, err
	}
	sum := sha256.New()
	h.order = append(h.order, name)
	h.hashes[name] = sum
	return io.MultiWriter(w, sum), nil
}

// writeManifest emits manifest.json listing the SHA-256 of every prior
// entry, in creation order. It is written directly to the underlying
// zip.Writer (never through the hasher — a manifest cannot digest
// itself) and must be the last entry created.
func (h *hashingZip) writeManifest() error {
	type entry struct {
		Name   string `json:"name"`
		SHA256 string `json:"sha256"`
	}
	files := make([]entry, 0, len(h.order))
	for _, name := range h.order {
		files = append(files, entry{Name: name, SHA256: hex.EncodeToString(h.hashes[name].Sum(nil))})
	}
	f, err := h.zw.Create(ManifestName)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"manifest_version": ManifestVersion,
		"algorithm":        "sha256",
		"files":            files,
	})
}

func (b *Builder) WriteTo(w io.Writer, opts Options) error {
	// R110: hold the export read lock for the whole sequence of store
	// reads below. Retention DELETEs (`DeleteSnapshotsOlderThan`,
	// `DeleteQueryRunsOlderThanByClass`) take the exclusive lock, so a
	// concurrent cleanup cycle cannot commit between this export's
	// reads and leave it referring to rows that have just been deleted
	// (e.g. the "missing result payload for successful run" tear).
	// Concurrent collection commits are not gated here — they only add
	// rows, so an export reading "old state" before a commit remains
	// internally consistent.
	release := b.store.LockExports()
	defer release()

	// Resolve the snapshot scope once, up front, into a per-call value.
	// Every writer below reads from this local scope so the six files
	// in the ZIP can never disagree about which cycles are in scope
	// (R084/R085) and two concurrent WriteTo calls cannot alias each
	// other's scope (R110, issue #323).
	scope, err := b.resolveScope(opts)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	// #455: tee every entry through SHA-256 so a per-file integrity
	// manifest can be written last.
	hz := newHashingZip(zw)

	if err := b.writeMetadata(hz, opts, scope); err != nil {
		return fmt.Errorf("write metadata.json: %w", err)
	}

	if err := b.writeCollectorStatus(hz, opts, scope); err != nil {
		return fmt.Errorf("write collector_status.json: %w", err)
	}

	if err := b.writeSnapshots(hz, opts, scope); err != nil {
		return fmt.Errorf("write snapshots.ndjson: %w", err)
	}

	if err := b.writeQueryCatalog(hz); err != nil {
		return fmt.Errorf("write query_catalog.json: %w", err)
	}

	if err := b.writeQueryRuns(hz, scope); err != nil {
		return fmt.Errorf("write query_runs.ndjson: %w", err)
	}

	if err := b.writeQueryResults(hz, scope); err != nil {
		return fmt.Errorf("write query_results.ndjson: %w", err)
	}

	if b.perCollectorFiles {
		if err := b.writePerCollectorFiles(hz, opts, scope); err != nil {
			return fmt.Errorf("write per-collector files: %w", err)
		}
	}

	// Manifest is the final entry: it digests every file written above
	// and cannot digest itself.
	if err := hz.writeManifest(); err != nil {
		return fmt.Errorf("write manifest.json: %w", err)
	}

	return nil
}

// resolveScope selects the snapshot rows that belong in the export
// according to R084/R085 semantics and returns them in a per-call
// exportScope value (issue #323: never stored on the shared Builder).
// The individual file writers all read from the returned scope, so the
// files in the ZIP cannot disagree.
//
//   - All && SnapshotID         → ErrConflictingSelectors.
//   - SnapshotID set            → that one snapshot, or
//     ErrSnapshotNotFound (FC-08).
//   - All / Since / Until set   → "broad" scope: all snapshots in
//     the time window, optionally
//     narrowed by target_id.
//   - default                   → R084: latest completed snapshot
//     per active target, optionally
//     narrowed by target_id.
func (b *Builder) resolveScope(opts Options) (*exportScope, error) {
	if opts.All && opts.SnapshotID != "" {
		return nil, ErrConflictingSelectors
	}

	scope := &exportScope{
		// Selector scopes are snapshot-based; the default scope
		// overrides this to "latest-per-collector" below (R086).
		runScope: "snapshot",
	}

	var snaps []db.Snapshot

	switch {
	case opts.SnapshotID != "":
		s, err := b.store.GetSnapshotByID(opts.SnapshotID)
		if err != nil {
			return nil, fmt.Errorf("lookup snapshot %q: %w", opts.SnapshotID, err)
		}
		if s == nil {
			return nil, fmt.Errorf("%w: %s", ErrSnapshotNotFound, opts.SnapshotID)
		}
		// Honour TargetID as a guard: requesting a snapshot that
		// belongs to a different target is treated as not found, so
		// an HTTP caller can't probe across targets via this path.
		if opts.TargetID > 0 && s.TargetID != opts.TargetID {
			return nil, fmt.Errorf("%w: %s (target_id mismatch)", ErrSnapshotNotFound, opts.SnapshotID)
		}
		snaps = []db.Snapshot{*s}

	case opts.All || opts.Since != "" || opts.Until != "":
		var err error
		if opts.TargetID > 0 {
			snaps, err = b.store.GetSnapshotsByTarget(opts.TargetID, opts.Since, opts.Until)
		} else {
			snaps, err = b.store.GetAllSnapshots(opts.Since, opts.Until)
		}
		if err != nil {
			return nil, fmt.Errorf("scan snapshots in scope: %w", err)
		}

	default:
		// R084 default: latest run of each collector per active target.
		// Resolve at the run level (not the snapshot level) so a target
		// whose collectors span cadences contributes the latest run of
		// every collector, not only those due in its newest cycle.
		runs, err := b.store.GetLatestRunsPerCollector(opts.TargetID)
		if err != nil {
			return nil, fmt.Errorf("latest runs per collector: %w", err)
		}
		scope.runScope = "latest-per-collector"
		scope.runSet = runs

		// Materialise the snapshots those runs belong to so
		// snapshots.ndjson and per-snapshot identity stay consistent.
		seen := make(map[string]bool, len(runs))
		var snapIDs []string
		for _, r := range runs {
			if r.SnapshotID == "" || seen[r.SnapshotID] {
				continue
			}
			seen[r.SnapshotID] = true
			snapIDs = append(snapIDs, r.SnapshotID)
		}
		snaps, err = b.store.GetSnapshotsByIDs(snapIDs)
		if err != nil {
			return nil, fmt.Errorf("snapshots for latest runs: %w", err)
		}
	}

	scope.snapshots = snaps
	scope.snapshotIDs = make(map[string]bool, len(snaps))
	for _, s := range snaps {
		scope.snapshotIDs[s.ID] = true
	}

	// Selector scopes draw their run set from the resolved snapshots;
	// the default scope already set runSet at the run level above.
	if scope.runScope == "snapshot" {
		ids := make([]string, 0, len(snaps))
		for _, s := range snaps {
			ids = append(ids, s.ID)
		}
		runs, err := b.store.GetQueryRunsBySnapshotIDs(ids)
		if err != nil {
			return nil, fmt.Errorf("runs for scoped snapshots: %w", err)
		}
		scope.runSet = runs
	}
	return scope, nil
}

func (b *Builder) writeMetadata(zw zipCreator, opts Options, scope *exportScope) error {
	f, err := zw.Create("metadata.json")
	if err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	data := map[string]any{
		"schema_version":                      snapshot.SchemaVersion,
		"collector_status_schema_version":     CollectorStatusSchemaVersion,
		"instance_id":                         b.instanceID,
		"signals_version":                     safety.Version,
		"collector_version":                   safety.Version, // legacy alias
		"collector_commit":                    safety.Commit,
		"generated_at":                        now,
		"collected_at":                        now, // legacy alias
		"unsafe_mode":                         b.unsafeMode,
		"high_sensitivity_collectors_enabled": b.highSensitivityCollectorsEnabled,
		// R086 — explicit scope intent for downstream consumers.
		// `snapshot_count` is the number of snapshots packaged in
		// this ZIP (0 on an empty store, 1 for --snapshot-id and
		// the typical R084 single-target default, N for --all or
		// multi-target default). `ingest_mode` is "analyze" for
		// every operator-driven export; "history_only" is reserved
		// for R087 backlog-replay traffic and is set only by the
		// (future) replay path.
		"snapshot_count": len(scope.snapshots),
		"ingest_mode":    "analyze",
		// R086: marks how runs were assembled — "latest-per-collector"
		// for the R084 default (runs may carry differing collected_at)
		// or "snapshot" for selector scopes.
		"run_scope": scope.runScope,
		// R125/INV-SIGNALS-25: an unambiguous, machine-detectable marker
		// so a consumer never has to infer emptiness or failure from an
		// absent snapshot row. "ok" when successful data is packaged;
		// "last_collection_failed" / "no_collection_yet" otherwise. The
		// default-scope endpoint refuses the two non-ok cases (R125);
		// the forensic --all scope is permissive but carries the marker.
		"collection_status": b.collectionStatus(scope),
	}
	if opts.TargetID > 0 {
		if name, err := b.store.GetTargetName(opts.TargetID); err == nil && name != "" {
			data["target_name"] = name
		}
		// R094: embed connection identity (host/port/dbname/username)
		// so an exported ZIP self-identifies its source cluster.
		// Carries no auth material (no sslmode, no secret_ref) per
		// INV-SIGNALS-07.
		//
		// Distinguish the R090 orphan case (target_id does not resolve)
		// from a transient DB error: orphan -> silently omit the field;
		// any other error -> fail the export so the operator sees the
		// problem instead of a misleadingly-clean ZIP with missing
		// identity rows.
		ident, err := b.store.GetTargetIdentity(opts.TargetID)
		switch {
		case err == nil:
			data["target_identity"] = ident
		case errors.Is(err, sql.ErrNoRows):
			// Orphan — fall through; target_identity stays absent.
		default:
			return fmt.Errorf("target_identity lookup for target_id=%d: %w", opts.TargetID, err)
		}
	}
	if b.unsafeMode && b.unsafeReasonsFunc != nil {
		reasons := b.unsafeReasonsFunc()
		if len(reasons) > 0 {
			data["unsafe_reasons"] = reasons
		}
	}
	return json.NewEncoder(f).Encode(data)
}

func (b *Builder) writeCollectorStatus(zw zipCreator, opts Options, scope *exportScope) error {
	f, err := zw.Create("collector_status.json")
	if err != nil {
		return err
	}

	// Pull runs from the resolved scope. R084/R085: every file in
	// the export draws from the same snapshot set, so
	// collector_status.json reflects only the cycles that actually
	// land in this ZIP — a fresh-install / latest-only export
	// reports just one cycle's collectors per target instead of the
	// daemon's accumulated history.
	scopedRuns := scope.runSet

	// Target-scoped: build status from query runs for that target (MTE-R004).
	if opts.TargetID > 0 {
		targetName := b.resolveTargetName(opts.TargetID)
		statuses := b.applyFreshness(collector.BuildStatusFromRuns(scopedRuns), scopedRuns, true, scope)

		file := collector.CollectorStatusFile{
			SchemaVersion: CollectorStatusSchemaVersion,
			TargetName:    targetName,
			CollectedAt:   time.Now().UTC().Format(time.RFC3339),
			Collectors:    statuses,
		}
		file.Sort()
		return json.NewEncoder(f).Encode(file)
	}

	// Instance-level: use the explicitly-supplied status if any.
	// (collectorStatus is daemon-managed and reflects the most-recent
	// cycle's per-collector outcomes, which the new latest-per-target
	// scope is precisely what the consumer expects.)
	if b.collectorStatus != nil {
		b.collectorStatus.Sort()
		return json.NewEncoder(f).Encode(b.collectorStatus)
	}

	// Unscoped export with no caller-supplied status: synthesise from
	// the scoped runs (post-0.3.1 H-002, refreshed for
	// R084/R085). The legacy behaviour was to write an empty
	// collectors[] array even when matching query runs existed, which
	// made auditors believe nothing had been collected. Synthesising
	// from runs keeps the file non-empty whenever the cycles actually
	// persisted runs. We make no attempt to dedupe across targets —
	// collectors that ran against multiple targets appear once per
	// target/run.
	file := collector.CollectorStatusFile{
		SchemaVersion: CollectorStatusSchemaVersion,
		CollectedAt:   time.Now().UTC().Format(time.RFC3339),
		Collectors:    b.applyFreshness(collector.BuildStatusFromRuns(scopedRuns), scopedRuns, false, scope),
	}
	if file.Collectors == nil {
		file.Collectors = []collector.CollectorStatus{}
	}
	file.Sort()
	return json.NewEncoder(f).Encode(file)
}

// applyFreshness adds R107 freshness metadata to collector_status
// entries. It only applies to the R084 default scope
// ("latest-per-collector"); selector-scoped exports are returned
// unchanged so a forensic --all / --snapshot-id export keeps its
// historical semantics.
//
// For each entry that actually ran (has a timestamp) it records the
// collector's expected cadence and classifies freshness: `fresh` when
// the latest run is at most twice the cadence old, `stale` otherwise.
// Gated/skipped entries get the cadence but no freshness (their
// `reason` already explains the absence). It then appends a
// `never_run` entry for every registered collector that has no run for
// a target in scope, so consumers can distinguish "ran and is current"
// from "never produced data."
//
// withNeverRun is set only for target-scoped exports: the flat,
// instance-level collector_status.json carries no target field, so a
// per-target `never_run` entry there would be ambiguous. Cadence and
// fresh/stale enrichment is unambiguous and applies to both.
func (b *Builder) applyFreshness(statuses []collector.CollectorStatus, runs []db.QueryRun, withNeverRun bool, scope *exportScope) []collector.CollectorStatus {
	if scope.runScope != "latest-per-collector" {
		return statuses
	}
	now := time.Now().UTC()

	for i := range statuses {
		qd := pgqueries.ByID(statuses[i].ID)
		if qd == nil {
			continue
		}
		statuses[i].Cadence = qd.Cadence.String()
		if statuses[i].Status == "skipped" || statuses[i].CollectedAt == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, statuses[i].CollectedAt)
		if err != nil {
			continue
		}
		if now.Sub(ts) <= 2*time.Duration(qd.Cadence) {
			statuses[i].Freshness = "fresh"
		} else {
			statuses[i].Freshness = "stale"
		}
	}

	if !withNeverRun {
		return statuses
	}

	// Enumerate eligible-but-never-run collectors per target. A
	// collector gated for a target is recorded as a skipped run every
	// cycle, so a registered collector with no run at all for a cycled
	// target is one whose cadence simply has not fired yet — exactly
	// the "missing coverage" case a consumer needs to see.
	presentByTarget := map[int64]map[string]bool{}
	for _, r := range runs {
		m := presentByTarget[r.TargetID]
		if m == nil {
			m = map[string]bool{}
			presentByTarget[r.TargetID] = m
		}
		m[r.QueryID] = true
	}
	for _, present := range presentByTarget {
		for _, q := range pgqueries.All() {
			if present[q.ID] {
				continue
			}
			statuses = append(statuses, collector.CollectorStatus{
				ID:        q.ID,
				Attempted: false,
				Status:    "never_run",
				Freshness: "never_run",
				Cadence:   q.Cadence.String(),
			})
		}
	}
	return statuses
}

func (b *Builder) resolveTargetName(targetID int64) string {
	name, err := b.store.GetTargetName(targetID)
	if err != nil || name == "" {
		return fmt.Sprintf("target-%d", targetID)
	}
	return name
}

func (b *Builder) writeQueryCatalog(zw zipCreator) error {
	f, err := zw.Create("query_catalog.json")
	if err != nil {
		return err
	}

	catalog, err := b.store.GetQueryCatalog()
	if err != nil {
		return err
	}
	return json.NewEncoder(f).Encode(catalog)
}

// writeQueryRuns emits one NDJSON row per query_run whose
// snapshot_id is in the resolved scope (R084/R085). The set is
// computed once by resolveScope and applies uniformly to runs and
// results so the two files cannot disagree.
func (b *Builder) writeQueryRuns(zw zipCreator, scope *exportScope) error {
	f, err := zw.Create("query_runs.ndjson")
	if err != nil {
		return err
	}

	runs := scope.runSet

	enc := json.NewEncoder(f)
	for _, r := range runs {
		row := map[string]any{
			"id":           r.ID,
			"target_id":    r.TargetID,
			"snapshot_id":  r.SnapshotID,
			"query_id":     r.QueryID,
			"collected_at": r.CollectedAt,
			"pg_version":   r.PGVersion,
			"duration_ms":  r.DurationMS,
			"row_count":    r.RowCount,
			"error":        r.Error,
			// R118 (#250): the persisted classification crosses the
			// snapshot boundary verbatim, so consumers read an R116
			// owner-only skip as a skip instead of re-deriving it
			// from the SQLSTATE in `error`.
			"status": r.Status,
			"reason": r.Reason,
		}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

// writeQueryResults emits one NDJSON row per successful run in the
// resolved scope (R084/R085).
func (b *Builder) writeQueryResults(zw zipCreator, scope *exportScope) error {
	f, err := zw.Create("query_results.ndjson")
	if err != nil {
		return err
	}

	runs := scope.runSet

	enc := json.NewEncoder(f)
	for _, r := range runs {
		// Skipped and failed runs legitimately have no payload.
		// Anything not status='success' (with the legacy fallback for
		// pre-status rows where status is empty and error is empty)
		// is allowed to be silently absent.
		isSuccess := r.Status == "success" || (r.Status == "" && r.Error == "")
		if !isSuccess {
			continue
		}

		res, err := b.store.GetQueryResultByRunID(r.ID)
		if err != nil {
			return fmt.Errorf("read result for run %s: %w", r.ID, err)
		}
		// A successful run that has no result payload is a data
		// integrity failure: InsertCollectionAtomic guarantees the
		// pair lands together, so a missing partner means the row
		// was deleted out of band or the storage corrupted.
		// post-0.3.1 M-001 — fail the export instead of silently
		// dropping the row, otherwise audits believe collection
		// produced no data when it actually did.
		if res == nil {
			return fmt.Errorf("missing result payload for successful run %s (query_id=%s)", r.ID, r.QueryID)
		}
		decoded, err := db.DecodeNDJSON(res.Payload, res.Compressed)
		if err != nil {
			return fmt.Errorf("decode result for run %s (query_id=%s): %w", r.ID, r.QueryID, err)
		}
		row := map[string]any{
			"run_id":  r.ID,
			"payload": decoded,
		}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

func (b *Builder) writeSnapshots(zw zipCreator, opts Options, scope *exportScope) error {
	f, err := zw.Create("snapshots.ndjson")
	if err != nil {
		return err
	}

	// Cache target_identity lookups within this export so a target
	// with N snapshots only queries `targets` once. `has=false`
	// means orphan (R090 case) — recorded so we don't re-query
	// repeatedly for the same orphan. Transient errors are NOT
	// cached — see below — so a flaky first call for a target
	// gets a second chance on the next snapshot.
	type cachedIdent struct {
		id  db.TargetIdentity
		has bool
	}
	identityCache := make(map[int64]cachedIdent)

	enc := json.NewEncoder(f)
	for _, s := range scope.snapshots {
		row := map[string]any{
			"id":           s.ID,
			"target_id":    s.TargetID,
			"collected_at": s.CollectedAt,
			"pg_version":   s.PGVersion,
			"payload":      json.RawMessage(s.Payload),
		}
		// R094: per-snapshot target_identity for multi-target exports
		// (and consistent in single-target exports too — additive). The
		// metadata.json top-level block is still authoritative for
		// single-target exports; this per-row field is what multi-
		// target consumers read to attribute each snapshot to its
		// originating cluster. Absent for orphan snapshots.
		//
		// Distinguish the R090 orphan case from a transient DB error
		// at lookup time. Orphan -> cache and silently omit. Any other
		// error -> fail the export. We do NOT cache transient errors,
		// so the export aborts on first occurrence; an operator's
		// retry then sees a fresh lookup.
		entry, cached := identityCache[s.TargetID]
		if !cached {
			ident, err := b.store.GetTargetIdentity(s.TargetID)
			switch {
			case err == nil:
				entry = cachedIdent{id: ident, has: true}
				identityCache[s.TargetID] = entry
			case errors.Is(err, sql.ErrNoRows):
				entry = cachedIdent{has: false}
				identityCache[s.TargetID] = entry
			default:
				return fmt.Errorf("target_identity lookup for target_id=%d: %w", s.TargetID, err)
			}
		}
		if entry.has {
			row["target_identity"] = entry.id
		}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}

	return nil
}

// writePerCollectorFiles regroups the export's already-resolved
// in-scope run set into one JSON file per (target_id, query_id) under
// `per-collector/<target_id>/`. R080: the canonical NDJSON layout
// remains authoritative; this directory is a derivative view for human
// browsing.
//
// Issue #322: the view is a STRICT regrouping of scope.runSet — the
// same runs that produced query_runs.ndjson / query_results.ndjson. It
// never re-queries the store with a different or narrower selector, so
// no per-collector run_id or payload can appear that is absent from the
// canonical files. Grouping by (target_id, query_id) preserves target
// attribution: a multi-target export keeps each target's run instead of
// one target silently winning a query_id-only grouping. A missing or
// corrupt payload for a successful in-scope run FAILS the export (same
// guard as writeQueryResults) rather than emitting a zero-row stub.
func (b *Builder) writePerCollectorFiles(zw zipCreator, opts Options, scope *exportScope) error {
	// Latest-run-wins per (target_id, query_id) from the resolved
	// scope. collected_at is RFC 3339 so a lexical compare matches time
	// order.
	type key struct {
		targetID int64
		queryID  string
	}
	latest := make(map[key]db.QueryRun, len(scope.runSet))
	for _, r := range scope.runSet {
		k := key{targetID: r.TargetID, queryID: r.QueryID}
		if cur, ok := latest[k]; !ok || r.CollectedAt > cur.CollectedAt {
			latest[k] = r
		}
	}

	// Stable file ordering inside the ZIP for deterministic output.
	keys := make([]key, 0, len(latest))
	for k := range latest {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].targetID != keys[j].targetID {
			return keys[i].targetID < keys[j].targetID
		}
		return keys[i].queryID < keys[j].queryID
	})

	// Cache target-name lookups so a multi-collector target resolves
	// its name once.
	nameCache := make(map[int64]string)

	for _, k := range keys {
		r := latest[k]

		entry := map[string]any{
			// run_id ties this file back to the canonical
			// query_runs.ndjson so consumers (and the #322 tests) can
			// verify the strict-regrouping invariant.
			"run_id":       r.ID,
			"target_id":    r.TargetID,
			"query_id":     r.QueryID,
			"collected_at": r.CollectedAt,
			"pg_version":   r.PGVersion,
			"duration_ms":  r.DurationMS,
			"row_count":    r.RowCount,
		}

		// Status: explicit if recorded; fall back to error-derived for
		// pre-migration rows. Mirrors collector.BuildStatusFromRuns.
		status := r.Status
		if status == "" {
			if r.Error != "" {
				status = "failed"
			} else {
				status = "success"
			}
		}
		entry["status"] = status
		if r.Reason != "" {
			entry["reason"] = r.Reason
		}
		if r.Error != "" {
			entry["detail"] = r.Error
		}

		name, ok := nameCache[r.TargetID]
		if !ok {
			name = b.resolveTargetName(r.TargetID)
			nameCache[r.TargetID] = name
		}
		entry["target_name"] = name

		// Row payload only for successful runs. Skipped/failed runs
		// describe themselves with status + reason/detail.
		//
		// Issue #322: for a successful in-scope run a missing or corrupt
		// payload is a data-integrity failure — fail the export with the
		// same guard writeQueryResults applies, never a zero-row stub.
		if status == "success" {
			res, err := b.store.GetQueryResultByRunID(r.ID)
			if err != nil {
				return fmt.Errorf("read result for run %s (query_id=%s): %w", r.ID, r.QueryID, err)
			}
			if res == nil {
				return fmt.Errorf("missing result payload for successful run %s (query_id=%s)", r.ID, r.QueryID)
			}
			rows, err := db.DecodeNDJSON(res.Payload, res.Compressed)
			if err != nil {
				return fmt.Errorf("decode result for run %s (query_id=%s): %w", r.ID, r.QueryID, err)
			}
			entry["rows"] = rows
		}

		f, err := zw.Create("per-collector/" + strconv.FormatInt(r.TargetID, 10) + "/" + r.QueryID + ".json")
		if err != nil {
			return err
		}
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(entry); err != nil {
			return err
		}
	}

	return nil
}
