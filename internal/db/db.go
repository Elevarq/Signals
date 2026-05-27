package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB with Arq Signals-specific operations.
type DB struct {
	sql *sql.DB
}

// Open creates or opens the SQLite database at path.
func Open(path string, wal bool) (*DB, error) {
	dsn := path + "?_busy_timeout=5000&_foreign_keys=on"
	if wal {
		dsn += "&_journal_mode=WAL"
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	sqlDB.SetMaxOpenConns(1)

	// Enable WAL via pragma as well (some drivers need this).
	if wal {
		if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL"); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("enable WAL: %w", err)
		}
	}

	return &DB{sql: sqlDB}, nil
}

// Close closes the underlying database.
func (d *DB) Close() error {
	return d.sql.Close()
}

// SQL returns the underlying *sql.DB for advanced use.
func (d *DB) SQL() *sql.DB {
	return d.sql
}

// Migrate runs all embedded SQL migrations in order.
func (d *DB) Migrate() error {
	// Create migration tracking table.
	if _, err := d.sql.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		filename TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		var applied int
		err := d.sql.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE filename = ?", entry.Name()).Scan(&applied)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", entry.Name(), err)
		}
		if applied > 0 {
			continue
		}

		data, err := fs.ReadFile(migrationsFS, migrationsDir+"/"+entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		slog.Info("applying migration", "file", entry.Name())
		if err := d.applyMigration(entry.Name(), string(data)); err != nil {
			return err
		}
	}

	return nil
}

// applyMigration runs a single migration file inside a transaction.
// Both the DDL statements and the schema_migrations insert are committed atomically.
func (d *DB) applyMigration(filename, sql string) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", filename, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(sql); err != nil {
		return fmt.Errorf("apply migration %s: %w", filename, err)
	}

	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (filename, applied_at) VALUES (?, ?)",
		filename, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("record migration %s: %w", filename, err)
	}

	return tx.Commit()
}

// ApplyMigrationSQL exposes applyMigration for testing.
func (d *DB) ApplyMigrationSQL(filename, sql string) error {
	return d.applyMigration(filename, sql)
}

// --- Meta CRUD ---

func (d *DB) GetMeta(key string) (string, error) {
	var val string
	err := d.sql.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

func (d *DB) SetMeta(key, value string) error {
	_, err := d.sql.Exec(
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value,
	)
	return err
}

// EnsureInstanceID returns the instance ID, generating one if it doesn't exist.
func (d *DB) EnsureInstanceID() (string, error) {
	id, err := d.GetMeta("instance_id")
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate instance id: %w", err)
	}
	id = hex.EncodeToString(b)
	if err := d.SetMeta("instance_id", id); err != nil {
		return "", err
	}
	slog.Info("generated instance ID", "id", id)
	return id, nil
}

// --- Events ---

func (d *DB) InsertEvent(eventType, detail string) error {
	_, err := d.sql.Exec(
		"INSERT INTO events (timestamp, event_type, detail) VALUES (?, ?, ?)",
		time.Now().UTC().Format(time.RFC3339), eventType, detail,
	)
	return err
}

// InsertTargetEvent inserts an event scoped to a specific target.
func (d *DB) InsertTargetEvent(targetID int64, eventType, detail string) error {
	_, err := d.sql.Exec(
		"INSERT INTO events (timestamp, event_type, detail, target_id) VALUES (?, ?, ?, ?)",
		time.Now().UTC().Format(time.RFC3339), eventType, detail, targetID,
	)
	return err
}

// --- Targets ---

type Target struct {
	ID         int64
	Name       string
	Host       string
	Port       int
	DBName     string
	Username   string
	SSLMode    string
	SecretType string
	SecretRef  string
	Enabled    bool
	CreatedAt  string
	UpdatedAt  string
}

func (d *DB) UpsertTarget(name string, host string, port int, dbname string, username string, sslmode string, secretType string, secretRef string, enabled bool) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := d.sql.Exec(`
		INSERT INTO targets (name, dsn_hash, host, port, dbname, username, sslmode, secret_type, secret_ref, enabled, created_at, updated_at)
		VALUES (?, '', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			host=excluded.host, port=excluded.port, dbname=excluded.dbname, username=excluded.username,
			sslmode=excluded.sslmode, secret_type=excluded.secret_type, secret_ref=excluded.secret_ref,
			enabled=excluded.enabled, updated_at=excluded.updated_at`,
		name, host, port, dbname, username, sslmode, secretType, secretRef, enabled, now, now,
	); err != nil {
		return 0, err
	}

	// R089: always SELECT the canonical id by name. SQLite's
	// `last_insert_rowid()` (Go's `Result.LastInsertId()`) is
	// unreliable for INSERT...ON CONFLICT...DO UPDATE: when the
	// DO UPDATE branch fires, SQLite reserves an AUTOINCREMENT id
	// for the INSERT branch, "wastes" it, but still returns it
	// from last_insert_rowid(). Trusting that wasted id and using
	// it as the snapshot's `target_id` is the v0.3.x bug that
	// produced 1,337 orphaned target_ids in a 17-hour daemon run.
	//
	// The SELECT below always returns the real `targets.id` — the
	// only reliable identity for the upserted row.
	var id int64
	if err := d.sql.QueryRow("SELECT id FROM targets WHERE name = ?", name).Scan(&id); err != nil {
		return 0, err
	}
	return id, nil
}

func (d *DB) GetTargets() ([]Target, error) {
	rows, err := d.sql.Query(`SELECT id, name, host, port, dbname, username, sslmode, secret_type, secret_ref, enabled, created_at, updated_at
		FROM targets ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var targets []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.ID, &t.Name, &t.Host, &t.Port, &t.DBName, &t.Username, &t.SSLMode, &t.SecretType, &t.SecretRef, &t.Enabled, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}

// --- Snapshots ---

type Snapshot struct {
	ID          string
	TargetID    int64
	CollectedAt string
	PGVersion   string
	Payload     json.RawMessage
	SizeBytes   int
}

func (d *DB) InsertSnapshot(s Snapshot) error {
	_, err := d.sql.Exec(
		"INSERT INTO snapshots (id, target_id, collected_at, pg_version, payload, size_bytes) VALUES (?, ?, ?, ?, ?, ?)",
		s.ID, s.TargetID, s.CollectedAt, s.PGVersion, string(s.Payload), s.SizeBytes,
	)
	return err
}

func (d *DB) GetSnapshotsByTarget(targetID int64, since, until string) ([]Snapshot, error) {
	query := "SELECT id, target_id, collected_at, pg_version, payload, size_bytes FROM snapshots WHERE target_id = ?"
	args := []any{targetID}
	if since != "" {
		query += " AND collected_at >= ?"
		args = append(args, since)
	}
	if until != "" {
		query += " AND collected_at <= ?"
		args = append(args, until)
	}
	query += " ORDER BY collected_at"

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snaps []Snapshot
	for rows.Next() {
		var s Snapshot
		var payload string
		if err := rows.Scan(&s.ID, &s.TargetID, &s.CollectedAt, &s.PGVersion, &payload, &s.SizeBytes); err != nil {
			return nil, err
		}
		s.Payload = json.RawMessage(payload)
		snaps = append(snaps, s)
	}
	return snaps, rows.Err()
}

func (d *DB) GetAllSnapshots(since, until string) ([]Snapshot, error) {
	query := "SELECT id, target_id, collected_at, pg_version, payload, size_bytes FROM snapshots WHERE 1=1"
	var args []any
	if since != "" {
		query += " AND collected_at >= ?"
		args = append(args, since)
	}
	if until != "" {
		query += " AND collected_at <= ?"
		args = append(args, until)
	}
	query += " ORDER BY collected_at"

	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var snaps []Snapshot
	for rows.Next() {
		var s Snapshot
		var payload string
		if err := rows.Scan(&s.ID, &s.TargetID, &s.CollectedAt, &s.PGVersion, &payload, &s.SizeBytes); err != nil {
			return nil, err
		}
		s.Payload = json.RawMessage(payload)
		snaps = append(snaps, s)
	}
	return snaps, rows.Err()
}

func (d *DB) CountSnapshots() (int, error) {
	var count int
	err := d.sql.QueryRow("SELECT COUNT(*) FROM snapshots").Scan(&count)
	return count, err
}

// GetSnapshotByID returns the snapshot with the given id, or nil if
// no row matches. Used by the export builder for the --snapshot-id
// selector (R085); a nil return is the producer-side signal for
// FC-08 (translated to HTTP 404 by the API layer / non-zero
// `arqctl` exit).
func (d *DB) GetSnapshotByID(id string) (*Snapshot, error) {
	var s Snapshot
	var payload string
	err := d.sql.QueryRow(
		"SELECT id, target_id, collected_at, pg_version, payload, size_bytes FROM snapshots WHERE id = ?",
		id,
	).Scan(&s.ID, &s.TargetID, &s.CollectedAt, &s.PGVersion, &payload, &s.SizeBytes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Payload = json.RawMessage(payload)
	return &s, nil
}

// GetLatestSnapshotsPerTarget returns one row per distinct target —
// the row with the largest `collected_at` for that target_id. Used
// by the R084 default export scope when no target filter is set.
// Result is ordered by target_id ascending so the export's
// snapshots.ndjson output is deterministic across calls.
//
// R090: the explicit `JOIN targets` filters out orphan
// `snapshots.target_id` values that don't reference an existing
// row in the targets table. This is defense-in-depth against
// pre-R089 stores that accumulated drift; orphans remain
// accessible via the `--all` selector for forensic exports.
func (d *DB) GetLatestSnapshotsPerTarget() ([]Snapshot, error) {
	const q = `
		SELECT s.id, s.target_id, s.collected_at, s.pg_version, s.payload, s.size_bytes
		FROM snapshots s
		JOIN targets t ON t.id = s.target_id
		JOIN (
			SELECT target_id, MAX(collected_at) AS max_at
			FROM snapshots
			GROUP BY target_id
		) latest ON latest.target_id = s.target_id AND latest.max_at = s.collected_at
		ORDER BY s.target_id ASC, s.id ASC`
	rows, err := d.sql.Query(q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// In the rare case two cycles for the same target landed at the
	// same `collected_at`, the JOIN above returns multiple rows for
	// that target. The id-tiebreak in ORDER BY keeps the result
	// deterministic; the caller (export builder) treats the first
	// row per target_id as canonical.
	var snaps []Snapshot
	seen := map[int64]bool{}
	for rows.Next() {
		var s Snapshot
		var payload string
		if err := rows.Scan(&s.ID, &s.TargetID, &s.CollectedAt, &s.PGVersion, &payload, &s.SizeBytes); err != nil {
			return nil, err
		}
		if seen[s.TargetID] {
			continue
		}
		seen[s.TargetID] = true
		s.Payload = json.RawMessage(payload)
		snaps = append(snaps, s)
	}
	return snaps, rows.Err()
}

// GetLatestSnapshotForTarget returns the snapshot with the largest
// `collected_at` for the given target_id, or nil if the target has
// no completed snapshot yet. Used by the R084 default scope when
// `--target-id` narrows the export to a single target.
//
// R090: the explicit `JOIN targets` ensures that requesting
// `--target-id` for an id that does not exist in the targets table
// (i.e., an orphan id from a pre-R089 store) returns no rows. The
// operator can fall back to `--all --target-id=<orphan>` to recover
// forensic visibility into corrupt history.
func (d *DB) GetLatestSnapshotForTarget(targetID int64) (*Snapshot, error) {
	var s Snapshot
	var payload string
	err := d.sql.QueryRow(
		`SELECT s.id, s.target_id, s.collected_at, s.pg_version, s.payload, s.size_bytes
		 FROM snapshots s
		 JOIN targets t ON t.id = s.target_id
		 WHERE s.target_id = ?
		 ORDER BY s.collected_at DESC, s.id DESC LIMIT 1`,
		targetID,
	).Scan(&s.ID, &s.TargetID, &s.CollectedAt, &s.PGVersion, &payload, &s.SizeBytes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	s.Payload = json.RawMessage(payload)
	return &s, nil
}

// GetLatestSnapshotTimeByTargetName returns the most recent
// `collected_at` for the target whose configured name matches.
// Returns (zeroTime, false, nil) when the target exists but has no
// completed snapshot yet (R091's first-cycle case) or when the
// target name is unknown.
//
// The query JOINs `targets` so orphaned `snapshots.target_id`
// values from a pre-R089 store are filtered out (R090
// defense-in-depth carried into this code path). Snapshots that
// reference a non-existent target row are not "completed
// snapshots" for the purposes of R091's interval check.
//
// Used by the collector (R091) to decide whether to skip a target
// because its min_snapshot_interval has not elapsed.
func (d *DB) GetLatestSnapshotTimeByTargetName(name string) (time.Time, bool, error) {
	var collectedAt string
	err := d.sql.QueryRow(
		`SELECT s.collected_at
		 FROM snapshots s
		 JOIN targets t ON t.id = s.target_id
		 WHERE t.name = ?
		 ORDER BY s.collected_at DESC, s.id DESC
		 LIMIT 1`,
		name,
	).Scan(&collectedAt)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	t, perr := time.Parse(time.RFC3339, collectedAt)
	if perr != nil {
		return time.Time{}, false, fmt.Errorf("parse collected_at %q: %w", collectedAt, perr)
	}
	return t, true, nil
}

// GetQueryRunsBySnapshotIDs returns every query_run whose
// snapshot_id is in the given set. Used by the export builder once
// the snapshot scope has been resolved (R084/R085). The empty input
// returns nil so the caller can write a zero-length
// query_runs.ndjson without an extra branch.
func (d *DB) GetQueryRunsBySnapshotIDs(ids []string) ([]QueryRun, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1] // drop trailing comma
	q := `SELECT id, target_id, snapshot_id, query_id, collected_at, pg_version, duration_ms, row_count, error, created_at, status, reason
	      FROM query_runs WHERE snapshot_id IN (` + placeholders + `) ORDER BY collected_at, id`
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := d.sql.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []QueryRun
	for rows.Next() {
		var r QueryRun
		if err := rows.Scan(
			&r.ID, &r.TargetID, &r.SnapshotID, &r.QueryID,
			&r.CollectedAt, &r.PGVersion, &r.DurationMS, &r.RowCount,
			&r.Error, &r.CreatedAt, &r.Status, &r.Reason,
		); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func (d *DB) DeleteSnapshotsOlderThan(before string) (int64, error) {
	res, err := d.sql.Exec("DELETE FROM snapshots WHERE collected_at < ?", before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// --- Query Catalog ---

type QueryCatalogRow struct {
	QueryID        string
	Category       string
	ResultKind     string
	RetentionClass string
	RegisteredAt   string
}

func (d *DB) UpsertQueryCatalog(row QueryCatalogRow) error {
	_, err := d.sql.Exec(`
		INSERT INTO query_catalog (query_id, category, result_kind, retention_class, registered_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(query_id) DO UPDATE SET
			category=excluded.category, result_kind=excluded.result_kind,
			retention_class=excluded.retention_class`,
		row.QueryID, row.Category, row.ResultKind, row.RetentionClass, row.RegisteredAt,
	)
	return err
}

func (d *DB) GetQueryCatalog() ([]QueryCatalogRow, error) {
	rows, err := d.sql.Query("SELECT query_id, category, result_kind, retention_class, registered_at FROM query_catalog ORDER BY query_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []QueryCatalogRow
	for rows.Next() {
		var r QueryCatalogRow
		if err := rows.Scan(&r.QueryID, &r.Category, &r.ResultKind, &r.RetentionClass, &r.RegisteredAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// --- Query Runs + Results ---

type QueryRun struct {
	ID          string
	TargetID    int64
	SnapshotID  string
	QueryID     string
	CollectedAt string
	PGVersion   string
	DurationMS  int
	RowCount    int
	Error       string
	CreatedAt   string
	// Status is one of "success", "failed", "skipped". Skipped runs have
	// Error empty and Reason populated (e.g. "config_disabled").
	Status string
	Reason string
}

type QueryResult struct {
	RunID      string
	Payload    []byte
	Compressed bool
	SizeBytes  int
}

func (d *DB) InsertQueryRunBatch(runs []QueryRun, results []QueryResult) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertRunsAndResults(tx, runs, results); err != nil {
		return err
	}
	return tx.Commit()
}

// InsertCollectionAtomic persists one collection cycle's snapshot, query
// runs, and query results inside a single transaction (R077). If any step
// fails the entire cycle is rolled back so partial state — e.g. a snapshot
// without its query runs, or runs without their result payloads — never
// becomes observable to readers or exports.
func (d *DB) InsertCollectionAtomic(snapshot Snapshot, runs []QueryRun, results []QueryResult) error {
	tx, err := d.sql.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		"INSERT INTO snapshots (id, target_id, collected_at, pg_version, payload, size_bytes) VALUES (?, ?, ?, ?, ?, ?)",
		snapshot.ID, snapshot.TargetID, snapshot.CollectedAt, snapshot.PGVersion, string(snapshot.Payload), snapshot.SizeBytes,
	); err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}

	if err := insertRunsAndResults(tx, runs, results); err != nil {
		return err
	}

	return tx.Commit()
}

// insertRunsAndResults shares the prepared-statement loop between the
// legacy batch insert and the atomic full-cycle insert.
func insertRunsAndResults(tx *sql.Tx, runs []QueryRun, results []QueryResult) error {
	if len(runs) == 0 && len(results) == 0 {
		return nil
	}

	runStmt, err := tx.Prepare(`INSERT INTO query_runs
		(id, target_id, snapshot_id, query_id, collected_at, pg_version, duration_ms, row_count, error, created_at, status, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer runStmt.Close()

	resStmt, err := tx.Prepare(`INSERT INTO query_results (run_id, payload, compressed, size_bytes) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer resStmt.Close()

	for _, r := range runs {
		status := r.Status
		if status == "" {
			if r.Error != "" {
				status = "failed"
			} else {
				status = "success"
			}
		}
		if _, err := runStmt.Exec(r.ID, r.TargetID, r.SnapshotID, r.QueryID, r.CollectedAt, r.PGVersion, r.DurationMS, r.RowCount, r.Error, r.CreatedAt, status, r.Reason); err != nil {
			return fmt.Errorf("insert run %s: %w", r.ID, err)
		}
	}

	for _, res := range results {
		comp := 0
		if res.Compressed {
			comp = 1
		}
		if _, err := resStmt.Exec(res.RunID, res.Payload, comp, res.SizeBytes); err != nil {
			return fmt.Errorf("insert result %s: %w", res.RunID, err)
		}
	}

	return nil
}

func (d *DB) GetAllQueryRuns(since, until string) ([]QueryRun, error) {
	query := "SELECT id, target_id, snapshot_id, query_id, collected_at, pg_version, duration_ms, row_count, error, created_at, status, reason FROM query_runs WHERE 1=1"
	var args []any
	if since != "" {
		query += " AND collected_at >= ?"
		args = append(args, since)
	}
	if until != "" {
		query += " AND collected_at <= ?"
		args = append(args, until)
	}
	query += " ORDER BY collected_at"
	return d.scanQueryRuns(query, args...)
}

// GetQueryRunsByTarget returns query runs filtered by target ID and
// optional time range (MTE-R001).
func (d *DB) GetQueryRunsByTarget(targetID int64, since, until string) ([]QueryRun, error) {
	query := "SELECT id, target_id, snapshot_id, query_id, collected_at, pg_version, duration_ms, row_count, error, created_at, status, reason FROM query_runs WHERE target_id = ?"
	args := []any{targetID}
	if since != "" {
		query += " AND collected_at >= ?"
		args = append(args, since)
	}
	if until != "" {
		query += " AND collected_at <= ?"
		args = append(args, until)
	}
	query += " ORDER BY collected_at"
	return d.scanQueryRuns(query, args...)
}

// GetTargetName returns the name for a target ID, or empty string.
func (d *DB) GetTargetName(targetID int64) (string, error) {
	var name string
	err := d.sql.QueryRow("SELECT name FROM targets WHERE id = ?", targetID).Scan(&name)
	if err != nil {
		return "", err
	}
	return name, nil
}

// TargetIdentity carries connection identity for a target row in an
// export-safe form: no secret reference, no sslmode, no auth material.
// Returned by GetTargetIdentity for R094 metadata.json embedding.
type TargetIdentity struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"dbname"`
	Username string `json:"username"`
}

// GetTargetIdentity returns the host/port/dbname/username for a target
// ID. Returns sql.ErrNoRows when the target_id does not resolve (the
// R090 orphan case) — callers distinguish via errors.Is. Deliberately
// excludes secret_type, secret_ref, and sslmode — those are auth
// material, not connection identity (INV-SIGNALS-07, R094).
//
// Value return (not pointer) matches the sibling style of
// GetTargetName(int64) (string, error). The struct is small and
// immutable from the reader's perspective.
func (d *DB) GetTargetIdentity(targetID int64) (TargetIdentity, error) {
	var t TargetIdentity
	err := d.sql.QueryRow(
		"SELECT host, port, dbname, username FROM targets WHERE id = ?",
		targetID,
	).Scan(&t.Host, &t.Port, &t.DBName, &t.Username)
	if err != nil {
		return TargetIdentity{}, err
	}
	return t, nil
}

func (d *DB) scanQueryRuns(query string, args ...any) ([]QueryRun, error) {
	rows, err := d.sql.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []QueryRun
	for rows.Next() {
		var r QueryRun
		if err := rows.Scan(&r.ID, &r.TargetID, &r.SnapshotID, &r.QueryID, &r.CollectedAt, &r.PGVersion, &r.DurationMS, &r.RowCount, &r.Error, &r.CreatedAt, &r.Status, &r.Reason); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *DB) GetQueryResultByRunID(runID string) (*QueryResult, error) {
	row := d.sql.QueryRow("SELECT run_id, payload, compressed, size_bytes FROM query_results WHERE run_id = ?", runID)
	var res QueryResult
	var comp int
	err := row.Scan(&res.RunID, &res.Payload, &comp, &res.SizeBytes)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	res.Compressed = comp == 1
	return &res, nil
}

func (d *DB) DeleteQueryRunsOlderThan(before string) (int64, error) {
	// Delete results first (FK dependency).
	_, err := d.sql.Exec(`DELETE FROM query_results WHERE run_id IN
		(SELECT id FROM query_runs WHERE collected_at < ?)`, before)
	if err != nil {
		return 0, err
	}
	res, err := d.sql.Exec("DELETE FROM query_runs WHERE collected_at < ?", before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteQueryRunsOlderThanByClass (R099) prunes query_runs whose
// owning collector's retention_class matches `class` (`short` /
// `medium` / `long`) and that are older than `before`. The
// retention_class is looked up via the `query_catalog` table.
//
// Snapshot rows are NOT touched here — they're pruned separately
// by DeleteSnapshotsOlderThan with the max-class cutoff so a
// snapshot stays alive as long as ANY class is still interested in
// its data.
func (d *DB) DeleteQueryRunsOlderThanByClass(class, before string) (int64, error) {
	_, err := d.sql.Exec(`
		DELETE FROM query_results
		WHERE run_id IN (
			SELECT qr.id
			FROM query_runs qr
			JOIN query_catalog qc ON qr.query_id = qc.query_id
			WHERE qc.retention_class = ? AND qr.collected_at < ?
		)`, class, before)
	if err != nil {
		return 0, err
	}
	res, err := d.sql.Exec(`
		DELETE FROM query_runs
		WHERE query_id IN (SELECT query_id FROM query_catalog WHERE retention_class = ?)
		  AND collected_at < ?`, class, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GetLastRunTimes returns the most recent successful collected_at per
// query_id for a target. Only status='success' rows advance cadence.
//
// Skipped runs (config_disabled / version_unsupported / extension_missing)
// and failed runs MUST NOT count: cadence is meant to throttle re-attempts
// of *successful* collection. A skipped or failed run that bumped the
// timestamp would defer the next legitimate attempt by a full cadence
// window, masking transient failures and gating misconfigurations
// behind invisible delays. Codex post-0.3.1 H-003.
//
// Legacy fallback: pre-status-column rows where status is empty are
// counted only when error is empty too — preserves cadence behaviour
// against databases that never ran the post-0.2 migration.
func (d *DB) GetLastRunTimes(targetID int64) (map[string]time.Time, error) {
	rows, err := d.sql.Query(
		`SELECT query_id, MAX(collected_at) FROM query_runs
		 WHERE target_id = ?
		   AND (status = 'success' OR (status = '' AND error = ''))
		 GROUP BY query_id`, targetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]time.Time)
	for rows.Next() {
		var qid, ts string
		if err := rows.Scan(&qid, &ts); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		out[qid] = t
	}
	return out, rows.Err()
}

// GetTargetLastCollected returns the most recent collected_at timestamp for the given target,
// checking both snapshots and query_runs tables. Returns empty string if none found.
func (d *DB) GetTargetLastCollected(targetID int64) string {
	var ts string
	// Check snapshots first.
	_ = d.sql.QueryRow(
		"SELECT collected_at FROM snapshots WHERE target_id = ? ORDER BY collected_at DESC LIMIT 1",
		targetID,
	).Scan(&ts)
	// Check query_runs for a potentially newer timestamp.
	var qrTS string
	_ = d.sql.QueryRow(
		"SELECT collected_at FROM query_runs WHERE target_id = ? ORDER BY collected_at DESC LIMIT 1",
		targetID,
	).Scan(&qrTS)
	if qrTS > ts {
		ts = qrTS
	}
	return ts
}
