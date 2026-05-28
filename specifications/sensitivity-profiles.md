# Per-Target Sensitivity Profiles

## Status

DRAFT

## Purpose

R075's `signals.high_sensitivity_collectors_enabled` is daemon-wide:
every `*_definitions_v1` collector either runs on every target or on
none. That coarse setting blocks production deployment in any fleet
that mixes regulated databases (where view / function / trigger
source text is sensitive) with unregulated ones (where it is not).

R098 introduces per-target profiles so operators can express the
realistic case:

```yaml
targets:
  - name: app
    profile: default          # all enabled collectors run
  - name: pii
    profile: restricted       # drops every collector tagged HighSensitivity
  - name: analytics
    profile: custom
    exclude:
      - pg_functions_definitions_v1
      - pg_triggers_definitions_v1
```

## Scope

Per-target collector profile, layered on top of the daemon-wide
gate. The daemon-wide gate is preserved unchanged for backward
compatibility — existing configs keep working byte-for-byte.

Built-in profiles:

| Profile | Behaviour |
|---------|-----------|
| `default` (or empty) | Inherit the daemon-wide configuration. Identical to today's behaviour. |
| `restricted` | Drop every collector flagged `HighSensitivity=true` for this target, regardless of the daemon-wide gate. |
| `custom` | Use explicit `include` / `exclude` lists. If both are present, the same collector ID in both is a config-validation error. |

`include` and `exclude` accept collector IDs (e.g. `pg_views_definitions_v1`).

## Inputs

`config.TargetConfig.Collectors` (under `collectors:` in YAML):

```yaml
targets:
  - name: app
    collectors:
      profile: restricted
```

Or:

```yaml
targets:
  - name: app
    collectors:
      profile: custom
      include:
        - pg_settings_v1
        - pg_stat_database_v1
      exclude:
        - pg_views_definitions_v1
```

`include` with `profile: custom` means **only** these IDs run for
this target — an explicit allowlist. `exclude` means these IDs are
removed from whatever the default eligible set would be.
`include` + `exclude` on different IDs combine; on the same ID,
`ValidateStrict` rejects.

## Filter precedence

For each (target, collector) pair, eligibility resolves in order:

1. **Version / extension gates** (R081). A collector that doesn't
   match the target's PG major or required extension is filtered out
   first, regardless of profile. (Same as today.)
2. **Daemon-wide `high_sensitivity_collectors_enabled`** (R075,
   revised 2026-05). Default `true` (collect-everything). When `false`:
   - HighSensitivity collectors with **empty/nil** `SensitiveColumns`
     (skip-path — DDL definitions, sampled-value stats, RLS policies,
     rewrite rules) are filtered out globally and reported as
     `status=skipped, reason=config_disabled`.
   - HighSensitivity collectors with **non-empty** `SensitiveColumns`
     (redact-path — the live `pg_stat_activity` collectors) stay
     eligible at this step; the named columns are NULL-ed in the
     persisted rows at execution time, preserving the non-sensitive
     diagnostic columns. The collector is **not** reported as
     `config_disabled` since it ran.
3. **Per-target profile** (R098, this slice). Applied on the
   per-target subset that survives steps 1 + 2. The `restricted`
   profile is **stricter** than the daemon-wide opt-out: it drops
   every `HighSensitivity=true` collector regardless of
   `SensitiveColumns` (no redaction substitute). Options:
   - `default` / empty: no change.
   - `restricted`: drop all `HighSensitivity=true`.
   - `custom` with `include`: keep only the listed IDs.
   - `custom` with `exclude`: drop the listed IDs.

A collector dropped at any step is reported in
`collector_status.json` for that target as
`status=skipped, reason=config_disabled`. From the analyzer's
perspective there is no observable difference between "gated by the
global flag" and "gated by per-target profile" — both are
operator-configured states (EA-R001).

## Invariants

- **INV-SENS-01**: Per-target profile NEVER expands eligibility beyond
  the daemon-wide gate. A target's `profile: default` does NOT enable
  HighSensitivity collectors when the daemon-wide flag is off — the
  global gate is the upper bound.
- **INV-SENS-02**: A profile string outside
  `{empty, default, restricted, custom}` is a
  `ValidateStrict` hard error at startup (FC-SENS-01).
- **INV-SENS-03**: With `profile: custom`, `include` and `exclude` may
  not name the same ID — `ValidateStrict` hard error
  (FC-SENS-02).
- **INV-SENS-04**: `include` / `exclude` referencing an unknown
  collector ID is a warning (not a hard error) — the catalog
  evolves and a future-release collector might be referenced by
  forward-looking configs.

## Failure Conditions

- **FC-SENS-01**: `signals.targets[].collectors.profile` is set to
  a value outside the supported set → startup config-validation
  error naming the target and the offending value.
- **FC-SENS-02**: `profile: custom` with the same collector ID in
  both `include` and `exclude` → startup error naming the conflict.

## Backward compatibility

- Targets without a `collectors:` block behave exactly as today.
- The daemon-wide `signals.high_sensitivity_collectors_enabled`
  flag is preserved and continues to gate at step 2 above.
- A future deprecation of the global flag (in favour of a default
  profile that's restricted) is **out of scope** for this slice.

## Sensitivity

Low. The profile structure carries no credential material and is
purely operational policy.

## Out of scope

- Adaptive profiles based on database content (e.g. \"if column
  names match `*_ssn`, auto-restrict\"). Operators set policy
  explicitly.
- Profile inheritance / templates. A target either picks a
  built-in profile or supplies an explicit `custom` block.
- Per-collector sensitivity grading beyond the existing
  `HighSensitivity bool`. A future `Sensitivity` enum could
  partition further (`medium` vs `high`), but the v1 surface is
  the same binary the codebase already uses.

## Analyzer requirements unblocked

- **Mixed-fleet deployment** in regulated environments. Operators
  in finance / healthcare / government can deploy a single daemon
  configured to skip view/function/trigger source on flagged
  databases while keeping full coverage elsewhere.
