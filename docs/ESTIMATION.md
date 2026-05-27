# Issue estimation

Every non-trivial issue in this repository carries two parallel sizings — a coarse **Effort** bucket and a numeric **Estimated Hours** value — plus a final **Actual Hours** recorded at close. The pair is how we calibrate future estimates: over time, the ratio between Actual and Estimated per bucket per area becomes a reliable signal.

This file is the canonical reference for the scale. The same convention applies across all actively-developed Elevarq engineering repositories.

## Effort scale

| Bucket | Hours range | Typical example |
|---|---|---|
| **XS** | 1–2 hours | Typo fix, one-line config change, a single test added |
| **S** | 3–8 hours | Small refactor, one collector, a single Go package change |
| **M** | 8–24 hours | Multi-file feature, new collector + tests, a focused spec |
| **L** | 24–80 hours | A subsystem (e.g. snapshot transport), spec + implementation pair |
| **XL** | 80+ hours | A milestone-shaped unit, multi-component subsystem, anything that warrants sub-issues |

## Why both Effort and Estimated Hours?

- **Effort (T-shirt size)** is the **planning-grain** unit. Easy to pick, easy to compare across issues, useful for capacity and sprint-grain decisions.
- **Estimated Hours** is the **calibration-grain** unit. Forces a concrete number rather than a hand-wave; it must sit inside the chosen Effort bucket's range, which means picking the number tightens the Effort choice.
- **Actual Hours** is recorded at close. The ratio `Actual / Estimated` is the calibration signal. Over time the team learns where its estimates systematically over- or under-shoot — that learning improves future Estimated Hours.

## Rules

1. **Every Effort bucket maps to an hours range.** Estimated Hours MUST sit inside the bucket's range. If you estimate 30 hours, the Effort is L (not M).
2. **Estimates are honest, not aspirational.** A 40-hour task is L; do not compress it to M to make a milestone look tidier. Calibration data is what you give up.
3. **Actual Hours is filled in at issue close.** If actual exceeded the estimate by more than 2× the bucket size, leave a one-line comment explaining the miss. That comment is the calibration improvement, not a self-criticism.
4. **An XL issue should usually be split into sub-issues.** XL exists for milestone-shaped tracking; the actual work usually decomposes into L and smaller pieces, each with its own estimate and close.

## Priorities

| Label | Meaning |
|---|---|
| `priority:P0` | Must-have. Blocks revenue or shipping. |
| `priority:P1` | Should-have for the current milestone window. |
| `priority:P2` | Nice-to-have; can be deferred. |
| `priority:P3` | Backlog / opportunistic. |

## How to fill the fields when opening an issue

The repository's issue templates ask for Effort, Estimated Hours, Actual Hours (leave blank), and Priority via dropdowns / text inputs. The values are captured in the issue body.

After creating the issue, apply the matching repo labels so the values are also indexable:

- `effort:XS` / `effort:S` / `effort:M` / `effort:L` / `effort:XL`
- `priority:P0` / `priority:P1` / `priority:P2` / `priority:P3`

(Label assignment is manual today. A future GitHub Action that reads the issue-form selection and applies the labels automatically is a possible follow-up; this file will be updated when/if it lands.)

## Closing an issue: filling Actual Hours

When you close the issue:

1. Edit the issue body and replace the placeholder "Actual hours" value with the integer (or decimal) number of hours actually spent.
2. If the variance from Estimated is significant (more than 2× the bucket size), add a comment explaining the cause (scope creep, hidden complexity, blocked by external party, etc.).
3. Close the issue normally (or via `Closes #<n>` in the merging PR — but make sure the body field is updated before close, since closed issues are rarely revisited).

## GitHub Projects custom fields (recommended setup)

The issue templates above capture the values in the issue body. For cross-issue rollup, filtering, and visual planning, a GitHub Project with matching custom fields is much more useful. GitHub Projects custom fields **cannot** be defined from repo files (the project lives at the organization or repo level, not in the source tree), so the setup is a one-time manual step per project.

Steps:

1. Open the Project view (`Projects` tab on the org or repo).
2. Click the `+` next to the column headers → **Add field**.
3. Create four custom fields:
   - **Effort** — single-select with options `XS`, `S`, `M`, `L`, `XL`.
   - **Estimated Hours** — number.
   - **Actual Hours** — number.
   - **Priority** — single-select with options `P0`, `P1`, `P2`, `P3`.
4. When triaging incoming issues into the Project, copy the values from the issue body into the Project fields. (This dual-recording is the manual cost we accept until label-automation lands.)
5. Save the Project's default view, or create board / table / roadmap views that group by Effort or Priority, sort by Estimated Hours, etc.

## Open questions / follow-ups

- An action that auto-applies effort / priority labels from issue-form selections.
- A close-time check that warns if Actual Hours is still blank.
- A monthly rollup (Estimated vs Actual across closed issues) for explicit calibration review.
