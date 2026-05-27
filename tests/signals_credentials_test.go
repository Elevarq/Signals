package tests

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/elevarq/arq-signals/internal/db"
)

// TestMigrationSQLNoPasswordColumn scans all migration SQL files to verify
// that no migration creates a "password" column. This ensures credentials
// are never persisted in the local database.
// Traces: ARQ-SIGNALS-R016 / TC-SIG-026
func TestMigrationSQLNoPasswordColumn(t *testing.T) {
	root := repoRoot(t)
	migrationsDir := filepath.Join(root, "internal", "db", "migrations")

	err := filepath.Walk(migrationsDir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".sql") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("reading %s: %v", path, readErr)
		}
		content := strings.ToLower(string(data))
		// Check for a column named "password" in any CREATE TABLE or ALTER TABLE.
		if strings.Contains(content, "password") {
			// Allow references in comments or event detail strings, but not column definitions.
			lines := strings.Split(content, "\n")
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				// Skip comment lines.
				if strings.HasPrefix(trimmed, "--") {
					continue
				}
				// Skip lines that are INSERT or UPDATE (event detail strings).
				if strings.HasPrefix(trimmed, "insert") || strings.HasPrefix(trimmed, "update") {
					continue
				}
				// Flag column definitions with "password".
				if strings.Contains(trimmed, "password") &&
					(strings.Contains(trimmed, "text") || strings.Contains(trimmed, "varchar") || strings.Contains(trimmed, "column")) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("migration %s line %d appears to define a password column: %q", rel, i+1, trimmed)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking migrations: %v", err)
	}
}

// TestTargetStructNoPasswordField verifies that the db.Target struct does not
// have a Password field. Credentials must never be stored in the local DB.
// Traces: ARQ-SIGNALS-R016 / TC-SIG-027
func TestTargetStructNoPasswordField(t *testing.T) {
	targetType := reflect.TypeOf(db.Target{})

	forbiddenFields := []string{"Password", "PasswordHash", "PasswordEncrypted", "Secret"}
	for _, fieldName := range forbiddenFields {
		if _, found := targetType.FieldByName(fieldName); found {
			t.Errorf("db.Target has forbidden field %q — credentials must not be stored", fieldName)
		}
	}
}

// TestExportQueryRunsNoPasswordField verifies that the query run export output
// structure does not contain password fields. The export only includes:
// id, target_id, snapshot_id, query_id, collected_at, pg_version, duration_ms, row_count, error.
// Traces: ARQ-SIGNALS-R016 / TC-SIG-028
func TestExportQueryRunsNoPasswordField(t *testing.T) {
	// Verify by inspecting the db.QueryRun struct fields.
	runType := reflect.TypeOf(db.QueryRun{})

	forbiddenFields := []string{"Password", "Credential", "Secret", "DSN"}
	for _, fieldName := range forbiddenFields {
		if _, found := runType.FieldByName(fieldName); found {
			t.Errorf("db.QueryRun has forbidden field %q — credentials must not be exported", fieldName)
		}
	}

	// Also verify the allowed field set is exactly what we expect.
	expectedFields := map[string]bool{
		"ID": true, "TargetID": true, "SnapshotID": true,
		"QueryID": true, "CollectedAt": true, "PGVersion": true,
		"DurationMS": true, "RowCount": true, "Error": true, "CreatedAt": true,
		"Status": true, "Reason": true,
	}

	for i := 0; i < runType.NumField(); i++ {
		name := runType.Field(i).Name
		if !expectedFields[name] {
			t.Errorf("db.QueryRun has unexpected field %q — review for credential exposure", name)
		}
	}
}

// TestMigrationEmbeddedFilesExist verifies that the embedded migration file
// list is non-empty (ensuring the embed directive works).
// Traces: ARQ-SIGNALS-R016 / TC-SIG-026
func TestMigrationEmbeddedFilesExist(t *testing.T) {
	names := db.MigrationFilenames()
	if len(names) == 0 {
		t.Fatal("no embedded migration files found")
	}
	for _, name := range names {
		if !strings.HasSuffix(name, ".sql") {
			t.Errorf("unexpected migration file: %s", name)
		}
	}
}
