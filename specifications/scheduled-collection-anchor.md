# Cron-Scheduled Collection Anchor

## Status

ACTIVE

## Issue

Elevarq/Signals#491 (part of the upgrade-lifecycle follow-ups; relates to #489
and the duration-units work #490). Design decisions confirmed with the product
owner 2026-10-13: **cron expression**, **UTC default with an opt-in
timezone**, **wait-for-next-slot with optional jitter** (no catch-up).

## Purpose

Signals' collection loop (`internal/collector`, `Run`) performs an immediate
baseline collection at process start and then ticks every `poll_interval`
anchored to **process start**. There is no way to say "collect daily at 02:00":
the phase is wherever the daemon happened to boot, and a restart re-anchors it.

This specification adds an **optional cron schedule** that anchors collection to
wall-clock times (daily/weekly/monthly at a chosen time) instead of to boot.
When a schedule is configured it **replaces** the `poll_interval` ticker; when
it is not, behaviour is **exactly** as before.

## Scope

In scope:

- A new `internal/schedule` package: parse a standard 5-field cron expression,
  resolve an IANA timezone (default UTC), and compute the next fire time after
  a given instant, with optional bounded jitter.
- Config fields `signals.schedule`, `signals.schedule_timezone`,
  `signals.schedule_jitter` (+ env equivalents), validated fail-fast at load.
- A collector option to install a schedule, and a `Run` branch that, when a
  schedule is installed, drives cycles from the schedule instead of the ticker.

Out of scope (explicit non-goals):

- Changing `poll_interval` semantics when no schedule is set.
- Catch-up of missed runs (a down daemon simply resumes at the next slot).
- Per-target schedules (one schedule governs the whole collector).
- Seconds-resolution cron, `@every`/`@daily` macros, or non-standard 6-field
  cron. Standard 5-field only.
- On-demand collection (`CollectNow`) behaviour — it remains always available.

## Definitions

- **Schedule** — a standard 5-field cron expression
  (`minute hour day-of-month month day-of-week`).
- **Anchor timezone** — the IANA location the cron fields are interpreted in.
  Empty ⇒ UTC.
- **Jitter** — a non-negative duration; each computed fire time is delayed by a
  uniformly-random amount in `[0, jitter)` so a fleet does not stampede a shared
  target at the exact anchor. Zero ⇒ no jitter.
- **Scheduled mode** — a non-empty `schedule` is configured. Otherwise
  **interval mode** (the pre-existing behaviour).

## Behaviors

### B1 — Interval mode unchanged (no schedule)

Given no `signals.schedule`, When the collector runs, Then it performs the
immediate baseline collection and then ticks every `poll_interval` anchored to
start — byte-for-byte the pre-existing behaviour.

### B2 — Scheduled mode waits for the next slot (no baseline)

Given a `signals.schedule`, When the collector runs, Then it does **not**
collect immediately at start; it waits until the next time the cron expression
matches (in the anchor timezone), collects then, and repeats at each subsequent
match.

### B3 — Schedule takes precedence over poll_interval

Given both `signals.schedule` and `signals.poll_interval` are set, When the
collector runs, Then the schedule governs cycle timing and `poll_interval` is
ignored (no ticker is armed).

### B4 — Timezone default and override

Given a schedule with no `schedule_timezone`, Then fire times are computed in
**UTC**. Given `schedule_timezone: <IANA name>`, Then fire times are computed in
that location (DST-aware), e.g. `0 2 * * *` fires at 02:00 local across DST.

### B5 — No catch-up across downtime; resume at next slot

Given the daemon was down across one or more scheduled slots, When it restarts,
Then it does **not** run the missed slots; it computes the next fire strictly
after "now" and waits for it.

### B6 — Jitter delays each fire within the bound

Given `schedule_jitter: J` (J > 0), When the next fire time `t` is computed,
Then the actual collection happens at `t + r` where `r` is in `[0, J)`; with
`J = 0` collection happens exactly at `t`.

### B7 — Fail-fast on an invalid schedule or timezone

Given an unparseable cron expression or an unknown timezone, When config is
loaded/validated, Then loading fails with a clear error naming the offending
value — the daemon never starts in a half-configured scheduling state.

### B8 — On-demand collection still works in scheduled mode

Given scheduled mode, When an on-demand collection is requested, Then it runs
immediately, independent of the schedule, exactly as in interval mode.

## Rules

- **R-SCHED-01**: A non-empty `signals.schedule` MUST be parsed as a standard
  5-field cron expression. An invalid expression MUST fail config validation
  (B7).
- **R-SCHED-02**: `signals.schedule_timezone`, when set, MUST resolve via the
  IANA tz database; an unknown name MUST fail config validation. Empty MUST mean
  UTC (R-SCHED default; B4).
- **R-SCHED-03**: `signals.schedule_jitter` MUST parse as a duration (the #490
  flexible-duration grammar, so `30s`/`5m`/`1h` and `1d` all work) and MUST be
  >= 0. A negative jitter MUST fail validation.
- **R-SCHED-04**: When a schedule is configured, the collector MUST NOT perform
  the start-time baseline collection and MUST NOT arm the `poll_interval` ticker
  (B2, B3).
- **R-SCHED-05**: The next fire MUST be strictly after the current instant,
  computed in the anchor timezone; the collector MUST NOT run slots that elapsed
  while it was down (B5).
- **R-SCHED-06**: Each fire MUST be delayed by a uniform random amount in
  `[0, jitter)`; `jitter == 0` MUST fire exactly at the computed time (B6).
- **R-SCHED-07**: When no schedule is configured, the loop MUST behave exactly
  as before (baseline + `poll_interval` ticker) — the feature is strictly
  additive (B1).
- **R-SCHED-08**: On-demand collection MUST remain available and immediate in
  both modes (B8).

## Invariants

- **INV-SCHED-01**: With no `signals.schedule`, no observable behaviour changes
  (additive feature).
- **INV-SCHED-02**: The daemon never starts with a configured-but-invalid
  schedule or timezone (fail-closed at load).
- **INV-SCHED-03**: A scheduled fire time is always strictly in the future
  relative to when it is computed; missed slots are never back-filled.
- **INV-SCHED-04**: This change introduces no credential handling and no new
  network egress; scheduling is local wall-clock logic only.

## Failure conditions

| Trigger | System response |
|---------|-----------------|
| Invalid cron expression in `signals.schedule` | Config load fails naming the value (R-SCHED-01 / B7). |
| Unknown `signals.schedule_timezone` | Config load fails naming the value (R-SCHED-02 / B7). |
| Negative or unparseable `signals.schedule_jitter` | Config load fails (R-SCHED-03). |
| Daemon down across scheduled slots | On restart, resume at the next slot; no catch-up (R-SCHED-05 / B5). |

## Constraints

- Standard 5-field cron only, parsed by a vetted library
  (`github.com/robfig/cron/v3`, `ParseStandard`) — not a hand-rolled parser.
- The `internal/schedule` package is deterministic and unit-testable: `now` and
  the jitter randomness are injected, so cron-Next, timezone, and jitter-bound
  behaviour are verified without sleeping or a live clock.

## NFR overrides

None. Standard Elevarq security/compliance baseline applies. Scheduling is
local logic; it adds no telemetry, no egress, and no credential surface
(INV-SCHED-04).

## Rollback / recovery

The feature is opt-in and additive: removing `signals.schedule` from config
restores interval mode on the next start. No data migration, no persisted state.
