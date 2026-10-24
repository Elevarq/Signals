package collector

import (
	"testing"

	"github.com/elevarq/signals/internal/config"
)

// #568 (reload isolation half): a target whose engine changes must NOT keep its
// cached connection pool — the engine selects the driver/backend, so the pool
// identity must differ. Conversely, identical configs (same engine) must share
// a pool so reload does not needlessly re-dial. Grounded in the Engine field
// added to connIdentity.

func TestReloadIsolation_EngineChangeInvalidatesPool(t *testing.T) {
	base := config.TargetConfig{Name: "t1", Host: "h", Port: 3306, User: "u", DBName: "d"}
	pg := base // Engine "" == PostgreSQL default
	ss := base
	ss.Engine = config.EngineSingleStore

	if sameConnection(pg, ss) {
		t.Fatal("#568: changing engine (postgresql->singlestore) must invalidate the pool")
	}
}

func TestReloadIsolation_IdenticalEngineSharesPool(t *testing.T) {
	a := config.TargetConfig{Name: "t1", Host: "h", Port: 3306, User: "u", DBName: "d", Engine: config.EngineSingleStore}
	b := a
	if !sameConnection(a, b) {
		t.Fatal("#568: identical SingleStore configs must share a pool (no needless re-dial)")
	}
}

func TestReloadIsolation_NonConnectionFieldPreservesPool(t *testing.T) {
	// Toggling a non-connection field (Enabled) must not invalidate the pool —
	// the engine and dialing fields are unchanged.
	a := config.TargetConfig{Name: "t1", Host: "h", Port: 3306, User: "u", DBName: "d", Engine: config.EngineSingleStore, Enabled: true}
	b := a
	b.Enabled = false
	if !sameConnection(a, b) {
		t.Fatal("#568: changing only Enabled must preserve the pool")
	}
}
