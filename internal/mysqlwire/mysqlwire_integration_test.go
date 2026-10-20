//go:build singlestore_integration

package mysqlwire

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/engineid"
)

// TC-WIRE-07 (integration): live SingleStore detection against the Dev Image.
// Guarded by the `singlestore_integration` build tag and skipped unless
// SINGLESTORE_TEST_DSN is set, so default CI never needs a live server (#555).
//
// Example:
//
//	SINGLESTORE_TEST_DSN='root:pass@tcp(127.0.0.1:3306)/' \
//	go test -tags singlestore_integration ./internal/mysqlwire/
func TestIdentify_LiveSingleStore(t *testing.T) {
	dsn := os.Getenv("SINGLESTORE_TEST_DSN")
	if dsn == "" {
		t.Skip("SINGLESTORE_TEST_DSN not set; skipping live SingleStore detection")
	}

	db, err := OpenDSN(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	engine, version, err := Identify(ctx, NewScalar(db))
	if err != nil {
		t.Fatalf("identify: %v", err)
	}
	if engine != engineid.EngineSingleStore {
		t.Fatalf("engine = %q, want singlestore", engine)
	}
	if version == "" {
		t.Fatalf("expected a non-empty SingleStore version")
	}
}
