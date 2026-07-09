package pgqueries

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const committedInventoryPath = "../../specifications/collectors/collector-inventory.json"

// TC-CINV-01 (R119, R121) — the committed inventory file is
// byte-identical to an in-memory regeneration from the registry.
// This is the CI drift gate: registering, renaming, or removing a
// collector without running `go run ./cmd/gen-collector-inventory`
// fails here.
func TestCollectorInventoryFileInSync(t *testing.T) {
	committed, err := os.ReadFile(filepath.FromSlash(committedInventoryPath))
	if err != nil {
		t.Fatalf("read committed inventory: %v (regenerate with `go run ./cmd/gen-collector-inventory`)", err)
	}
	if err := CompareCollectorInventory(committed); err != nil {
		t.Fatalf("committed inventory drifted from registry: %v\nregenerate with `go run ./cmd/gen-collector-inventory`", err)
	}
	fresh, err := CollectorInventoryJSON()
	if err != nil {
		t.Fatalf("CollectorInventoryJSON: %v", err)
	}
	if !bytes.Equal(committed, fresh) {
		t.Fatalf("committed inventory is not byte-identical to regeneration (non-canonical bytes?)\nregenerate with `go run ./cmd/gen-collector-inventory`")
	}
}

// TC-CINV-02 (R120) — canonical, byte-stable encoding: repeated
// regeneration is byte-identical, entries sorted by name, exactly
// one trailing newline, no volatile fields.
func TestCollectorInventoryCanonicalEncoding(t *testing.T) {
	a, err := CollectorInventoryJSON()
	if err != nil {
		t.Fatalf("CollectorInventoryJSON: %v", err)
	}
	b, err := CollectorInventoryJSON()
	if err != nil {
		t.Fatalf("CollectorInventoryJSON (second call): %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("two regenerations differ — encoding is not byte-stable")
	}
	if !bytes.HasSuffix(a, []byte("\n")) || bytes.HasSuffix(a, []byte("\n\n")) {
		t.Fatal("document must end in exactly one trailing newline")
	}
	for _, volatile := range []string{"generated_at", "source_ref", "signals_version"} {
		if strings.Contains(string(a), volatile) {
			t.Fatalf("volatile field %q must not appear in the inventory", volatile)
		}
	}
	var doc struct {
		Collectors []struct {
			Category string `json:"category"`
			Name     string `json:"name"`
		} `json:"collectors"`
		ContractVersion int `json:"contract_version"`
	}
	if err := json.Unmarshal(a, &doc); err != nil {
		t.Fatalf("regenerated inventory is not valid JSON: %v", err)
	}
	if doc.ContractVersion != 1 {
		t.Fatalf("contract_version = %d, want 1 (R122)", doc.ContractVersion)
	}
	for i := 1; i < len(doc.Collectors); i++ {
		if doc.Collectors[i-1].Name >= doc.Collectors[i].Name {
			t.Fatalf("entries not strictly sorted by name: %q before %q",
				doc.Collectors[i-1].Name, doc.Collectors[i].Name)
		}
	}
}

// TC-CINV-03 (R119) — the inventory carries every registered ID
// exactly once with its registered category.
func TestCollectorInventoryMatchesRegistry(t *testing.T) {
	raw, err := CollectorInventoryJSON()
	if err != nil {
		t.Fatalf("CollectorInventoryJSON: %v", err)
	}
	var doc struct {
		Collectors []struct {
			Category string `json:"category"`
			Name     string `json:"name"`
		} `json:"collectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	registered := All()
	if len(doc.Collectors) != len(registered) {
		t.Fatalf("inventory has %d entries, registry has %d", len(doc.Collectors), len(registered))
	}
	byID := make(map[string]string, len(registered))
	for _, q := range registered {
		byID[q.ID] = q.Category
	}
	seen := make(map[string]bool, len(doc.Collectors))
	for _, c := range doc.Collectors {
		if seen[c.Name] {
			t.Fatalf("duplicate inventory entry %q", c.Name)
		}
		seen[c.Name] = true
		cat, ok := byID[c.Name]
		if !ok {
			t.Fatalf("inventory entry %q is not a registered collector", c.Name)
		}
		if c.Category != cat {
			t.Fatalf("inventory entry %q has category %q, registry says %q", c.Name, c.Category, cat)
		}
	}
}

// TC-CINV-04 (R121) — drift reports name the missing and extra IDs.
func TestCollectorInventoryDriftIsNamed(t *testing.T) {
	raw, err := CollectorInventoryJSON()
	if err != nil {
		t.Fatalf("CollectorInventoryJSON: %v", err)
	}
	var doc struct {
		Collectors []struct {
			Category string `json:"category"`
			Name     string `json:"name"`
		} `json:"collectors"`
		ContractVersion int `json:"contract_version"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Collectors) == 0 {
		t.Fatal("registry is empty — cannot exercise drift")
	}
	dropped := doc.Collectors[0].Name
	doc.Collectors[0].Name = "not_a_registered_collector_v1"
	mutated, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal mutated doc: %v", err)
	}
	err = CompareCollectorInventory(mutated)
	if err == nil {
		t.Fatal("CompareCollectorInventory accepted a drifted inventory")
	}
	msg := err.Error()
	if !strings.Contains(msg, dropped) {
		t.Errorf("drift error does not name the missing ID %q: %v", dropped, err)
	}
	if !strings.Contains(msg, "not_a_registered_collector_v1") {
		t.Errorf("drift error does not name the extra ID: %v", err)
	}
}

// TC-CINV-05 (R121) — malformed committed bytes fail with a parse
// error, not a panic.
func TestCollectorInventoryMalformedFile(t *testing.T) {
	if err := CompareCollectorInventory([]byte("{not json")); err == nil {
		t.Fatal("CompareCollectorInventory accepted malformed JSON")
	}
}
