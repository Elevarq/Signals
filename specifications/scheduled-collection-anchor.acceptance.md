# Acceptance — Cron-Scheduled Collection Anchor (#491)

Spec: `scheduled-collection-anchor.md`. Each case references a spec rule/behavior
and maps to a test in `internal/schedule/schedule_test.go` or
`internal/config/schedule_config_test.go`.

```yaml
- id: AC-SCHED-01            # normal — daily cron, UTC default
  ref: [R-SCHED-01, R-SCHED-02, B4]
  given: schedule "0 2 * * *", no timezone, now 2026-10-13T08:00:00Z
  when: next fire computed
  then: 2026-10-14T02:00:00Z (next 02:00 UTC, strictly after now)
  test: TestSchedule_NextDailyUTC

- id: AC-SCHED-02            # normal — explicit timezone (DST-aware)
  ref: [R-SCHED-02, B4]
  given: schedule "0 2 * * *", timezone "Europe/Amsterdam", now a summer instant
  when: next fire computed
  then: the next 02:00 Amsterdam local time (00:00Z in CEST)
  test: TestSchedule_NextWithTimezone

- id: AC-SCHED-03            # boundary — next is STRICTLY after now (no catch-up)
  ref: [R-SCHED-05, B5, INV-SCHED-03]
  given: schedule "0 2 * * *", now exactly 2026-10-13T02:00:00Z
  when: next fire computed
  then: 2026-10-14T02:00:00Z (the current match is not re-run)
  test: TestSchedule_NextIsStrictlyAfterNow

- id: AC-SCHED-04            # boundary — jitter stays within [0, J)
  ref: [R-SCHED-06, B6]
  given: schedule "0 2 * * *", jitter 10m, deterministic rand
  when: next fire computed repeatedly
  then: every result is in [base, base+10m); jitter 0 yields exactly base
  test: TestSchedule_JitterWithinBound, TestSchedule_ZeroJitterExact

- id: AC-SCHED-05            # invalid — bad cron expression
  ref: [R-SCHED-01, B7]
  given: schedule "not a cron"
  when: config validated
  then: load fails naming signals.schedule
  test: TestScheduleConfig_RejectsBadCron

- id: AC-SCHED-06            # invalid — unknown timezone
  ref: [R-SCHED-02, B7]
  given: schedule "0 2 * * *", timezone "Mars/Olympus"
  when: config validated
  then: load fails naming signals.schedule_timezone
  test: TestScheduleConfig_RejectsBadTimezone

- id: AC-SCHED-07            # invalid — negative jitter
  ref: [R-SCHED-03]
  given: schedule set, schedule_jitter "-5m"
  when: config validated
  then: load fails naming signals.schedule_jitter
  test: TestScheduleConfig_RejectsNegativeJitter

- id: AC-SCHED-08            # failure/compat — no schedule ⇒ interval mode
  ref: [R-SCHED-07, INV-SCHED-01, B1]
  given: no signals.schedule
  when: config validated and collector constructed
  then: no scheduler installed; poll_interval governs; behaviour unchanged
  test: TestScheduleConfig_EmptyIsIntervalMode
```
