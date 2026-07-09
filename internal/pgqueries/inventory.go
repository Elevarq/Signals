package pgqueries

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// collectorInventoryContractVersion identifies the schema of the
// committed inventory document (R122). Adding fields or changing the
// encoding is a deliberate spec amendment that bumps it.
const collectorInventoryContractVersion = 1

// inventoryEntry is one collector in the canonical inventory. Field
// names sort alphabetically so json.Marshal of the struct emits the
// same key order json.Marshal of a sorted map would (R120).
type inventoryEntry struct {
	Category string `json:"category"`
	Name     string `json:"name"`
}

type inventoryDoc struct {
	Collectors      []inventoryEntry `json:"collectors"`
	ContractVersion int              `json:"contract_version"`
}

// CollectorInventoryJSON returns the canonical collector inventory
// document: one entry per registered QueryDef, sorted ascending by
// name, two-space indentation, trailing newline, no volatile fields
// (R119/R120). Byte-identical across calls for an unchanged
// registry — this is what `go run ./cmd/gen-collector-inventory`
// commits and what the R121 CI gate compares against.
func CollectorInventoryJSON() ([]byte, error) {
	defs := All()
	entries := make([]inventoryEntry, 0, len(defs))
	for _, q := range defs {
		entries = append(entries, inventoryEntry{Category: q.Category, Name: q.ID})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	raw, err := json.MarshalIndent(inventoryDoc{
		Collectors:      entries,
		ContractVersion: collectorInventoryContractVersion,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal collector inventory: %w", err)
	}
	return append(raw, '\n'), nil
}

// CompareCollectorInventory checks committed inventory bytes against
// the live registry and reports drift with the missing and extra
// collector IDs named (R121). It compares the name sets, not bytes:
// byte-level canonicality is asserted separately by regenerating and
// comparing, so this error can stay actionable ("regenerate") rather
// than a raw diff.
func CompareCollectorInventory(committed []byte) error {
	var doc inventoryDoc
	if err := json.Unmarshal(committed, &doc); err != nil {
		return fmt.Errorf("parse committed collector inventory: %w", err)
	}
	committedNames := make(map[string]bool, len(doc.Collectors))
	for _, e := range doc.Collectors {
		committedNames[e.Name] = true
	}
	var missing, extra []string
	registered := make(map[string]bool)
	for _, q := range All() {
		registered[q.ID] = true
		if !committedNames[q.ID] {
			missing = append(missing, q.ID)
		}
	}
	for name := range committedNames {
		if !registered[name] {
			extra = append(extra, name)
		}
	}
	if len(missing) == 0 && len(extra) == 0 {
		return nil
	}
	sort.Strings(missing)
	sort.Strings(extra)
	var b strings.Builder
	b.WriteString("collector inventory drifted from registry:")
	if len(missing) > 0 {
		fmt.Fprintf(&b, " missing from inventory: %s;", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, " not registered: %s;", strings.Join(extra, ", "))
	}
	return fmt.Errorf("%s", strings.TrimSuffix(b.String(), ";"))
}
