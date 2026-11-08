package collector

import (
	"context"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/config"
)

// #715 / F13 / SIGNALS-R100.2: a stale in-flight cycle holding a pre-reload
// target must not be able to install or reuse a pool carrying the OLD
// connection identity after a reload changed that target — otherwise later,
// current cycles reuse the stale pool by name and dial the old host / old
// credential indefinitely. This is distinct from R100.1 (#328) field-comparison
// completeness and reproduces even when reload correctly tears the pool down.

func poolID(t *testing.T, c *Collector, name string) (connIdentity, bool) {
	t.Helper()
	c.poolsMu.Lock()
	defer c.poolsMu.Unlock()
	pc, ok := c.pools[name]
	return pc.id, ok
}

func TestReload_StaleCycleCannotInstallOldConfigPool(t *testing.T) {
	c := New(tempStore(t), nil, time.Minute, 0)

	old := config.TargetConfig{
		Name: "pg", Host: "old.invalid", Port: 5432, DBName: "db", User: "u",
		SSLMode: "disable", Enabled: true,
	}
	if err := c.Reload([]config.TargetConfig{old}); err != nil {
		t.Fatalf("Reload(old): %v", err)
	}

	// A cycle acquires the pool under the old config (lazy — no dial).
	if _, err := c.getPool(context.Background(), old); err != nil {
		t.Fatalf("getPool(old): %v", err)
	}
	if id, ok := poolID(t, c, "pg"); !ok || id.Host != "old.invalid" {
		t.Fatalf("pool not installed with old identity: ok=%v host=%v", ok, id.Host)
	}

	// Reload retargets the SAME name to a new host — reload closes the modified
	// pool (R100.1).
	newT := old
	newT.Host = "new.invalid"
	if err := c.Reload([]config.TargetConfig{newT}); err != nil {
		t.Fatalf("Reload(new): %v", err)
	}
	if _, ok := poolID(t, c, "pg"); ok {
		t.Fatalf("reload did not close the modified pool (R100.1 precondition)")
	}

	// The stale in-flight cycle, still holding `old`, calls getPool. It MUST be
	// refused — it must not re-install the old-host pool as the name's pool.
	if _, err := c.getPool(context.Background(), old); err == nil {
		t.Fatal("getPool accepted a stale (pre-reload) target — it would reinstall the old-config pool for later current cycles (#715)")
	}
	if _, ok := poolID(t, c, "pg"); ok {
		t.Fatal("a stale cycle installed a pool after reload — must be refused (#715/R100.2)")
	}

	// The current cycle (new config) builds the pool with the NEW identity.
	if _, err := c.getPool(context.Background(), newT); err != nil {
		t.Fatalf("getPool(new): %v", err)
	}
	id, ok := poolID(t, c, "pg")
	if !ok {
		t.Fatal("current cycle did not install a pool")
	}
	if id.Host != "new.invalid" {
		t.Errorf("pool dials %q after reload, want new.invalid — a stale cycle installed the old pool (#715)", id.Host)
	}
	if id != connectionIdentity(newT) {
		t.Errorf("pool identity != current config identity: got host=%q", id.Host)
	}
}

// getPool reuses a pool only when the requester's connection identity matches
// the stored pool's identity (reuse-side of R100.2).
func TestGetPool_ReusesOnlyOnIdentityMatch(t *testing.T) {
	c := New(tempStore(t), nil, time.Minute, 0)
	tgt := config.TargetConfig{
		Name: "pg", Host: "h", Port: 5432, DBName: "db", User: "u",
		SSLMode: "disable", Enabled: true,
	}
	if err := c.Reload([]config.TargetConfig{tgt}); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	p1, err := c.getPool(context.Background(), tgt)
	if err != nil {
		t.Fatalf("getPool #1: %v", err)
	}
	// Same identity -> same pool reused.
	p2, err := c.getPool(context.Background(), tgt)
	if err != nil {
		t.Fatalf("getPool #2: %v", err)
	}
	if p1 != p2 {
		t.Error("getPool rebuilt the pool for an unchanged identity — should reuse")
	}
}
