//go:build singlestore_integration

package singlestore

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/mysqlwire"
)

// TC-SSC-LIVE: run the real catalog against a live SingleStore instance and
// assert no collector fatally errors and identity reports a version. This is
// how the catalog SQL/columns are validated. Guarded by the
// singlestore_integration tag; skipped unless SINGLESTORE_TEST_DSN is set.
//
//	SINGLESTORE_TEST_DSN='admin:...@tcp(host:3306)/?tls=true' \
//	go test -tags singlestore_integration -run TestCollect_Live -v ./internal/singlestore/
func TestCollect_Live(t *testing.T) {
	dsn := os.Getenv("SINGLESTORE_TEST_DSN")
	if dsn == "" {
		t.Skip("SINGLESTORE_TEST_DSN not set")
	}
	db, err := mysqlwire.OpenDSN(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	results := Collect(ctx, NewRunner(db), Catalog(), false)
	for _, r := range results {
		if r.Err != "" {
			t.Errorf("collector %s errored: %s", r.CollectorID, r.Err)
		} else {
			t.Logf("[live] %-32s rows=%d", r.CollectorID, len(r.Rows))
		}
	}
	id := resultByID(results, "singlestore_identity_v1")
	if id == nil || len(id.Rows) != 1 || id.Rows[0]["memsql_version"] == nil || id.Rows[0]["memsql_version"] == "" {
		t.Fatalf("identity collector must report a SingleStore version, got %+v", id)
	}
}
