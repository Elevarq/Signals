# Acceptance Tests: Data-Directory Writability Preflight

## Feature

`specifications/data-dir-writable-preflight.md`

## Test Cases

### TC-DDW-01: Writable directory passes

**Rule:** RULE-DDW-01 / RULE-DDW-02 — Normal

**Scenario:** The data directory exists and is writable by the current user.

**Given:**
- A writable directory `dir`.

**When:**
- `PreflightWritable(filepath.Join(dir, "signals.db"))` is called.

**Then:**
- It returns `nil`.

**Test:** `TestPreflightWritable_WritableDir`

---

### TC-DDW-02: Missing directory fails clearly

**Rule:** Failure condition — Boundary

**Scenario:** The parent directory of `database.path` does not exist.

**Given:**
- A path whose parent directory is absent.

**When:**
- `PreflightWritable(path)` is called.

**Then:**
- It returns a non-nil error naming that the directory "does not exist".

**Test:** `TestPreflightWritable_MissingDir`

---

### TC-DDW-03: Non-writable directory fails without blaming WAL

**Rule:** Failure condition / RULE-DDW-03 — Invalid

**Scenario:** The data directory exists but is not writable by the current
uid (the shipped non-root posture over a root-owned bind mount).

**Given:**
- A directory created mode `0o500` (not writable), run as non-root.

**When:**
- `PreflightWritable(filepath.Join(dir, "signals.db"))` is called.

**Then:**
- It returns a non-nil error.
- The message contains "not writable" and names the "uid".
- The message does **not** contain "wal".

**Test:** `TestPreflightWritable_NotWritable`
