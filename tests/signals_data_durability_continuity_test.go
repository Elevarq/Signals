// Copyright (c) 2026 Scantr LLC. All rights reserved.
// Elevarq is a trade name of Scantr LLC.
// This file is part of Elevarq Signals. Use is governed by the
// commercial license at LICENSE in the repository root.

// Store-layer continuity guard for cloud /data durability (#501).
//
// The IaC durability work (tests/data_durability_test.go) proves the durable
// volume is DECLARED. This test proves the store BEHAVES correctly when that
// durable /data is carried to a replacement instance: a fresh daemon over the
// same store file must see the SAME instance identity, the SAME collection
// history, and the SAME cadence watermark — so collection continues
// incrementally, not from an empty database. This is the store-layer model of
// the B2/B3 replacement scenario that IaC alone cannot exercise in CI.
//
// Spec: specifications/cloud-data-durability.md (B2, B3, INV-DUR-02, INV-DUR-03)
// Acceptance: TC-DUR-08
package tests

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/elevarq/signals/internal/db"
)

// TestDataDurability_StoreSurvivesInstanceReplacement models instance/AMI
// replacement at the store layer: the durable /data volume (here, a file on
// disk) is populated by a first collector, the "instance" is destroyed (store
// closed), and a replacement collector opens the SAME file. Identity, history,
// and watermark must all carry over.
func TestDataDurability_StoreSurvivesInstanceReplacement(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "signals.db") // the durable /data volume

	const targetName = "pg1"
	var (
		firstID     string
		targetID    int64
		wantRuns    int
		wantWMID    = "pg_stat_statements_v1"
		wantWMStamp = "2026-10-12T10:00:00Z"
	)

	// --- Instance #1: run a cycle, persist identity + history + watermark. ---
	func() {
		store, err := db.Open(dbPath, false)
		if err != nil {
			t.Fatalf("instance1 db.Open: %v", err)
		}
		defer func() { _ = store.Close() }()
		if err := store.Migrate(); err != nil {
			t.Fatalf("instance1 Migrate: %v", err)
		}

		firstID, err = store.EnsureInstanceID()
		if err != nil {
			t.Fatalf("instance1 EnsureInstanceID: %v", err)
		}
		if firstID == "" {
			t.Fatal("instance1 produced an empty instance_id")
		}

		targetID, err = store.UpsertTarget(targetName, "db.example.com", 5432, "postgres", "signals", "verify-full", "", "", true)
		if err != nil {
			t.Fatalf("instance1 UpsertTarget: %v", err)
		}

		// Persist two cycles of history, the second carrying the watermark run.
		for i, stamp := range []string{"2026-10-12T09:55:00Z", wantWMStamp} {
			snapID := fmt.Sprintf("snap-%d", i)
			snap := db.Snapshot{ID: snapID, TargetID: targetID, CollectedAt: stamp, PGVersion: "17.0", Payload: []byte(`{}`), SizeBytes: 2}
			run := db.QueryRun{
				ID: fmt.Sprintf("run-%d", i), TargetID: targetID, SnapshotID: snapID,
				QueryID: wantWMID, CollectedAt: stamp, PGVersion: "17.0",
				CreatedAt: stamp, Status: "success",
			}
			if err := store.InsertCollectionAtomic(snap, []db.QueryRun{run}, nil); err != nil {
				t.Fatalf("instance1 InsertCollectionAtomic[%d]: %v", i, err)
			}
			wantRuns++
		}
	}()

	// --- Instance replacement: fresh daemon over the SAME durable store. ---
	store, err := db.Open(dbPath, false)
	if err != nil {
		t.Fatalf("instance2 db.Open (re-attach durable /data): %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(); err != nil {
		t.Fatalf("instance2 Migrate: %v", err)
	}

	// INV-DUR-02: identity carries over — NOT a freshly generated id.
	gotID, err := store.EnsureInstanceID()
	if err != nil {
		t.Fatalf("instance2 EnsureInstanceID: %v", err)
	}
	if gotID != firstID {
		t.Errorf("instance identity reset on replacement: got %q, want %q (INV-DUR-02)", gotID, firstID)
	}

	// INV-DUR-02: collection history carries over.
	gotSnaps, err := store.CountSnapshots()
	if err != nil {
		t.Fatalf("instance2 CountSnapshots: %v", err)
	}
	if gotSnaps != wantRuns {
		t.Errorf("snapshot history lost on replacement: got %d, want %d (INV-DUR-02)", gotSnaps, wantRuns)
	}
	gotRuns, err := store.GetQueryRunsByTarget(targetID, "", "")
	if err != nil {
		t.Fatalf("instance2 GetQueryRunsByTarget: %v", err)
	}
	if len(gotRuns) != wantRuns {
		t.Errorf("query-run history lost on replacement: got %d, want %d (INV-DUR-02)", len(gotRuns), wantRuns)
	}

	// INV-DUR-02: cadence / incremental watermark carries over, so the next
	// cycle is incremental rather than a from-zero re-collect.
	wm, err := store.GetLastRunTimes(targetID)
	if err != nil {
		t.Fatalf("instance2 GetLastRunTimes: %v", err)
	}
	got, ok := wm[wantWMID]
	if !ok {
		t.Fatalf("watermark for %q lost on replacement — next cycle would re-collect from zero (INV-DUR-02)", wantWMID)
	}
	want, _ := time.Parse(time.RFC3339, wantWMStamp)
	if !got.Equal(want) {
		t.Errorf("watermark drifted on replacement: got %s, want %s (INV-DUR-02)", got, want)
	}

	// INV-DUR-04 / INV-SIGNALS-07: no credential was persisted into the store
	// as a side effect of durability. The target row carries only a by-value
	// secret reference type/ref, never a password/token.
	for _, k := range []string{"password", "api_token", "token", "secret", "db_password"} {
		v, err := store.GetMeta(k)
		if err != nil {
			t.Fatalf("instance2 GetMeta(%q): %v", k, err)
		}
		if v != "" {
			t.Errorf("credential-like meta key %q present in store (INV-DUR-04 / INV-SIGNALS-07)", k)
		}
	}
}
