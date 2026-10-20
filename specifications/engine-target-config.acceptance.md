# Acceptance Tests: engine-target-config

## Feature

`specifications/engine-target-config.md` — per-target `engine` field,
PostgreSQL default, validated against the supported set.

## Test Cases

### TC-ETC-01: Absent engine defaults to PostgreSQL (normal, compat)

**Rule:** ETC-R001, INV-ETC-01

**Given:** `baseValidTarget()` with no `Engine`.

**When:** `tgt.EffectiveEngine()` is called and `ValidateStrict(baseValidConfig(tgt))` runs.

**Then:** `EffectiveEngine() == EnginePostgreSQL`; `ValidateStrict` returns no error.

**Expected Result:** Pass when existing configs are unchanged and behave as PostgreSQL.

---

### TC-ETC-02: engine=singlestore accepted (boundary)

**Rule:** ETC-R003

**Given:** `baseValidTarget()` with `Engine = EngineSingleStore`.

**When:** `tgt.EffectiveEngine()` is called and `ValidateStrict(baseValidConfig(tgt))` runs.

**Then:** `EffectiveEngine() == "singlestore"`; `ValidateStrict` returns no error.

**Expected Result:** Pass when a supported non-PostgreSQL engine validates.

---

### TC-ETC-03: Unsupported engine rejected (invalid)

**Rule:** ETC-R002

**Given:** `baseValidTarget()` with `Engine = "oracle"`.

**When:** `ValidateStrict(baseValidConfig(tgt))` runs.

**Then:** a non-nil error is returned whose message mentions `engine` and `oracle`.

**Expected Result:** Pass when an unsupported engine is a hard error.

---

### TC-ETC-04: Engine matching is exact (invalid)

**Rule:** INV-ETC-02

**Given:** `baseValidTarget()` with `Engine = "SingleStore"` (wrong case).

**When:** `ValidateStrict(baseValidConfig(tgt))` runs.

**Then:** a non-nil error is returned (no case normalization; must match the
lowercase constant).

**Expected Result:** Pass when only the exact lowercase engine value is accepted.
