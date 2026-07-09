# Acceptance Tests: Canonical Collector Inventory

## Feature

`specifications/collector-inventory.md`

## Test Cases

### TC-CINV-01: Committed inventory matches the registry

**Rule:** R119, R121 (normal)

**Scenario:** The committed
`specifications/collectors/collector-inventory.json` is compared
against an in-memory regeneration from the live registry.

**Given:**
- The registry as populated by package `init()`.

**When:**
- The inventory is regenerated in memory and compared byte-for-byte
  with the committed file.

**Then:**
- The bytes are identical.

**Expected Result:** Pass.
(`TestCollectorInventoryFileInSync`)

---

### TC-CINV-02: Encoding is canonical and byte-stable

**Rule:** R120 (boundary)

**Scenario:** Two consecutive in-memory regenerations.

**Given:**
- The registry as populated by package `init()`.

**When:**
- `CollectorInventoryJSON()` is called twice.

**Then:**
- Both outputs are byte-identical, entries are sorted ascending by
  `name`, the document ends in exactly one trailing newline, and no
  volatile field (`generated_at`, `source_ref`, …) is present.

**Expected Result:** Pass.
(`TestCollectorInventoryCanonicalEncoding`)

---

### TC-CINV-03: Inventory carries every registered ID exactly once

**Rule:** R119 (boundary)

**Scenario:** Set comparison between inventory names and registry
IDs.

**Given:**
- The regenerated inventory document parsed back from its JSON
  bytes.

**When:**
- Its `collectors[].name` values are compared with
  `pgqueries.All()` IDs.

**Then:**
- Same cardinality, set-equal, no duplicates; every entry's
  `category` equals the registered `QueryDef.Category`.

**Expected Result:** Pass.
(`TestCollectorInventoryMatchesRegistry`)

---

### TC-CINV-04: Drift fails with named collectors

**Rule:** R121 (failure)

**Scenario:** The committed file diverges from the registry (as if a
collector was registered without regenerating).

**Given:**
- A synthetic committed document missing one registered ID and
  carrying one unregistered ID.

**When:**
- The sync comparison helper runs against it.

**Then:**
- It reports failure and the report names both the missing and the
  extra ID.

**Expected Result:** Pass.
(`TestCollectorInventoryDriftIsNamed`)

---

### TC-CINV-05: Malformed committed file fails the gate

**Rule:** R121 (invalid)

**Scenario:** The committed file is not valid JSON.

**Given:**
- A synthetic committed document containing invalid JSON bytes.

**When:**
- The sync comparison helper runs against it.

**Then:**
- It reports failure (parse error), not a panic.

**Expected Result:** Pass.
(`TestCollectorInventoryMalformedFile`)
