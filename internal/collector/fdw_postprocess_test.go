// Tests for the FDW row post-processor.
//
// The post-processor must:
//   - replace the targeted option `text[]` column with a redacted map
//     for every fdw_* collector ID
//   - leave non-FDW collectors untouched
//   - degrade gracefully on missing / wrong-shape values
//
// Spec: specifications/collectors/fdw_*_v1.md (REDACT-R001..R004).

package collector

import (
	"reflect"
	"testing"
)

func TestRedactFDWRows_NonFDWCollectorIsNoOp(t *testing.T) {
	rows := []map[string]any{
		{
			"name":           "vector",
			"installed":      true,
			"server_options": []any{"password=hunter2"}, // looks FDW-shaped, but ID isn't a FDW collector
		},
	}
	redactFDWRowsIfNeeded("extension_inventory_v1", rows)
	// The lookalike column should be untouched: the post-processor
	// keys off the COLLECTOR ID, not the column name.
	got := rows[0]["server_options"]
	if !reflect.DeepEqual(got, []any{"password=hunter2"}) {
		t.Errorf("non-FDW collector row got mutated: %v", got)
	}
}

func TestRedactFDWRows_WrappersV1RedactsFDWOptions(t *testing.T) {
	rows := []map[string]any{
		{
			"fdw_oid":       int64(16384),
			"fdw_name":      "postgres_fdw",
			"fdw_owner":     "postgres",
			"fdw_handler":   "postgres_fdw_handler",
			"fdw_validator": "postgres_fdw_validator",
			"fdw_options":   []any{"key=public_value", "password=hunter2"},
		},
	}
	redactFDWRowsIfNeeded("fdw_wrappers_v1", rows)

	opts, ok := rows[0]["fdw_options"].(map[string]string)
	if !ok {
		t.Fatalf("fdw_options not converted to map[string]string; got %T (%v)", rows[0]["fdw_options"], rows[0]["fdw_options"])
	}
	// `key=` matches the redactor's `key` pattern → redacted.
	if opts["key"] != "<redacted>" {
		t.Errorf("`key` should be redacted (matches the `key` pattern); got %q", opts["key"])
	}
	if opts["password"] != "<redacted>" {
		t.Errorf("password should be redacted; got %q", opts["password"])
	}
}

func TestRedactFDWRows_ServersV1RedactsServerOptions(t *testing.T) {
	rows := []map[string]any{
		{
			"server_oid":     int64(16385),
			"server_name":    "remote_pg",
			"fdw_name":       "postgres_fdw",
			"server_options": []any{"host=db.example.com", "port=5432", "dbname=production"},
		},
	}
	redactFDWRowsIfNeeded("fdw_servers_v1", rows)
	opts, ok := rows[0]["server_options"].(map[string]string)
	if !ok {
		t.Fatalf("server_options not map[string]string; got %T", rows[0]["server_options"])
	}
	if opts["host"] != "db.example.com" || opts["port"] != "5432" || opts["dbname"] != "production" {
		t.Errorf("non-sensitive options should round-trip; got %v", opts)
	}
}

// Critical safety test: the load-bearing case for the whole feature.
// User-mapping options carry passwords; redaction must replace the
// value with `<redacted>` and leave the key intact for downstream
// reasoning ("we know you set a password" without revealing it).
func TestRedactFDWRows_UserMappingsV1RedactsPasswords(t *testing.T) {
	rows := []map[string]any{
		{
			"server_name":     "remote_pg",
			"local_user_name": "postgres",
			"mapping_options": []any{"user=remote_reader", "password=hunter2", "sslmode=require"},
		},
	}
	redactFDWRowsIfNeeded("fdw_user_mappings_v1", rows)

	opts, ok := rows[0]["mapping_options"].(map[string]string)
	if !ok {
		t.Fatalf("mapping_options not map[string]string; got %T", rows[0]["mapping_options"])
	}
	if opts["password"] != "<redacted>" {
		t.Errorf("CRITICAL: password not redacted in user-mapping options; got %q (full map: %v)", opts["password"], opts)
	}
	if opts["user"] != "remote_reader" {
		t.Errorf("remote `user` is not a secret; should round-trip. got %q", opts["user"])
	}
	if opts["sslmode"] != "require" {
		t.Errorf("sslmode should round-trip; got %q", opts["sslmode"])
	}
	// Defensive: make sure the cleartext is nowhere in the row.
	for k, v := range opts {
		if v == "hunter2" {
			t.Errorf("CRITICAL: cleartext password leaked under key %q", k)
		}
	}
}

func TestRedactFDWRows_ForeignTablesV1RedactsOptions(t *testing.T) {
	rows := []map[string]any{
		{
			"schemaname":            "public",
			"table_name":            "remote_accounts",
			"server_name":           "remote_pg",
			"fdw_name":              "postgres_fdw",
			"foreign_table_options": []any{"schema_name=accounting", "table_name=accounts", "fetch_size=100"},
		},
	}
	redactFDWRowsIfNeeded("fdw_foreign_tables_v1", rows)
	opts := rows[0]["foreign_table_options"].(map[string]string)
	if opts["schema_name"] != "accounting" || opts["table_name"] != "accounts" || opts["fetch_size"] != "100" {
		t.Errorf("non-sensitive foreign_table_options should round-trip; got %v", opts)
	}
}

// Defensive: the post-processor must not crash on rows that lack
// the option column or carry an unexpected shape. A future SQL
// override that drops/renames the column should degrade silently
// rather than corrupt the run.
func TestRedactFDWRows_DegradesGracefullyOnMissingColumn(t *testing.T) {
	rows := []map[string]any{
		{"server_name": "x", "fdw_name": "y"}, // no server_options column
	}
	redactFDWRowsIfNeeded("fdw_servers_v1", rows)
	// No panic; the row is left as-is.
	if _, has := rows[0]["server_options"]; has {
		t.Errorf("post-processor should not synthesise a missing column")
	}
}

func TestRedactFDWRows_DegradesGracefullyOnNilColumn(t *testing.T) {
	rows := []map[string]any{
		{"server_name": "x", "fdw_name": "y", "server_options": nil},
	}
	redactFDWRowsIfNeeded("fdw_servers_v1", rows)
	// nil stays nil — never panics, never produces an empty map.
	if rows[0]["server_options"] != nil {
		t.Errorf("nil column should remain nil; got %v", rows[0]["server_options"])
	}
}

func TestRedactFDWRows_DegradesGracefullyOnWrongShape(t *testing.T) {
	rows := []map[string]any{
		{"server_name": "x", "fdw_name": "y", "server_options": "not an array"},
	}
	redactFDWRowsIfNeeded("fdw_servers_v1", rows)
	// Should leave the unexpected value untouched rather than corrupt.
	if rows[0]["server_options"] != "not an array" {
		t.Errorf("unexpected shape should be left as-is; got %v", rows[0]["server_options"])
	}
}

func TestRedactFDWRows_HandlesEmptyArray(t *testing.T) {
	rows := []map[string]any{
		{"server_name": "x", "fdw_name": "y", "server_options": []any{}},
	}
	redactFDWRowsIfNeeded("fdw_servers_v1", rows)
	opts, ok := rows[0]["server_options"].(map[string]string)
	if !ok {
		t.Fatalf("expected map[string]string; got %T", rows[0]["server_options"])
	}
	if len(opts) != 0 {
		t.Errorf("empty options array should produce empty map; got %v", opts)
	}
}
