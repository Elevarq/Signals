# Marketplace AMI / EC2 Image Builder Groundwork

## Status

ACTIVE

## Type

Integration mapping — contract between the Signals collector's EC2 run mechanics
and an AWS EC2 Image Builder component that bakes the collector into a golden
AMI. The live `AmiProduct@1.0` listing that exposes it is governed by the sibling
spec `specifications/marketplace-ami-product.md`. Tests mandatory for the
component build input; the live listing's standup/versioning is out of scope here
(see below).

## Purpose

Lay the **reusable groundwork** for offering Signals as an AWS Marketplace AMI /
server product (#235, part of #233), without standing up the live product. The
substantive deliverable is an **EC2 Image Builder component** that installs and
configures the collector during an AMI bake, mirroring the existing
`deploy/aws/terraform` EC2 run path (docker-run the published image under a
systemd unit) — so a customer baking a golden AMI gets the collector
pre-installed.

Standing up the live `AmiProduct@1.0` listing, building/maintaining the AMI, and
onboarding/review were originally deferred behind #235's demand gate ("only
pursue if there is real demand for non-container, EC2-baked deployment"). That
**gate opened 2026-07-19**: the listing is live at `prod-cuyands3nsl2c` and is
now governed by `specifications/marketplace-ami-product.md`. This spec governs
the reusable component that is the AMI's **build input**; the live product's
standup and versioning live in that sibling spec.

## Scope

In scope (shipped now):

- `deploy/aws/imagebuilder/signals-collector-component.yaml` — an AWSTOE
  (`schemaVersion: 1.0`) component with `build`, `validate`, and `test` phases.
- `deploy/aws/imagebuilder/README.md` — how the component works, and the
  `AmiProduct@1.0` publish path (Catalog API `CreateProduct` +
  `AddDeliveryOptions` with `AmiDeliveryOptionDetails`/`AmiSource` — the component
  is the AMI *build input*, not a delivery field; the live standup is #235,
  governed by `specifications/marketplace-ami-product.md`).
- This spec + acceptance cases.

Out of scope (governed by `specifications/marketplace-ami-product.md`, not here):

- Baking, publishing, or maintaining the live AMI (this spec governs only the
  component that is the AMI's build input).
- The live `AmiProduct@1.0` listing standup, onboarding/review, and its
  per-release `start-change-set` versioning (`07-add-ami-delivery.json`).

## Interfaces

### Image Builder component

| Element | Value / constraint |
|---------|--------------------|
| `name` | `signals-collector` |
| `schemaVersion` | `1.0` |
| Parameter `SignalsImage` | string; the published Signals image ref at a **pinned** version (no `latest`). Baked into the AMI at build time. |
| `build` phase | Install docker + enable it; pre-pull `SignalsImage`; create `/etc/signals`; install a `signals.service` systemd unit that docker-runs the image with `--restart=always` and `--network host` (R-AMI-07), mounting `/etc/signals:ro` and a `signals-data` volume. Enables the unit (starts on boot). |
| `validate` phase | Assert docker is installed and enabled, the image is present locally, and `signals.service` is installed and enabled. |
| `test` phase | Assert the unit is enabled and the baked image digest matches `SignalsImage`. |

## Rules

- **R-AMI-01**: The component MUST NOT bake any credentials, API token, database
  config, or TLS material into the AMI. Config (`/etc/signals/signals.yaml`, CA
  file) and the control-plane token are supplied by the buyer **at launch**
  (user-data / SSM), never at bake — same credentials-by-reference posture as the
  container listings and the Elevarq security baseline.
- **R-AMI-02**: `SignalsImage` MUST be a pinned version tag (no `latest`), so the
  AMI is reproducible and traceable to a released image.
- **R-AMI-03**: The component mirrors the `deploy/aws/terraform` run contract
  (docker-run the image, `/etc/signals` config dir, `signals-data` volume,
  non-root uid 10001 inside the container, `--network host` per R-AMI-07) — it
  does not invent a second, divergent EC2 install path.
- **R-AMI-07** (host networking — #488): Every EC2 run path (the Image Builder
  component, `deploy/aws/terraform`, and `deploy/aws/cloudformation`) MUST run
  the collector container with `--network host`, so it uses the instance ENI
  directly. On instances with **jumbo-frame ENIs (MTU 9001 — the in-VPC default
  for most instance types)**, a Docker **bridge** container's TLS handshake to
  RDS black-holes across the 9001->1500 MTU boundary: TCP connects, then the
  session hangs until the collection timeout (observed as `query role
  attributes: context deadline exceeded`). Host networking avoids the bridge
  MTU boundary entirely. The collector only makes **outbound** connections and
  binds its API on `127.0.0.1:8081`, which under host networking is the host's
  loopback — so **no `-p` publish is used** (a `-p` bridge publish is incompatible
  with host networking and reintroduces the bug). This keeps all three EC2 paths
  identical (INV-AMI-03).
- **R-AMI-04**: The live `AmiProduct@1.0` listing is a **separate product** with
  its own onboarding/review. The #235 demand gate **opened 2026-07-19** (product
  owner un-deferred it; the listing is live at `prod-cuyands3nsl2c`), so its
  standup and per-release versioning are now governed by
  `specifications/marketplace-ami-product.md` and driven by the canonical
  change-set template `docs/marketplace/catalog-api/07-add-ami-delivery.json`.
  The component in this spec remains the reproducible **build input** for that
  AMI, not a delivery field; any committed AMI change-set template MUST be
  fully `${...}`-parameterized (no hardcoded `ami-*` id, seller account, or
  secret) so it cannot itself trigger an unreviewed live change.
- **R-AMI-05** (env passthrough — #292): The unit MUST forward the **whole**
  buyer-supplied `/etc/signals/signals.env` to the container via docker's
  `--env-file /etc/signals/signals.env`, so **any** `SIGNALS_*` variable a buyer
  places there (not only `SIGNALS_API_TOKEN`) reaches the collector. The
  `deploy/aws/terraform` and `deploy/aws/cloudformation` IaC run paths forward
  the same env file the same way for parity (INV-AMI-03). **Decision (#292,
  forward-all vs document-only):**
  forward-all — chosen 2026-07-20 by Frank. Rationale: the collector already
  reads its full runtime config from `SIGNALS_*` env (`internal/config`), so a
  buyer tuning any of those (e.g. the dev-only `SIGNALS_ALLOW_INSECURE_PG_TLS`
  that surfaced this in the #235 AMI launch-test) must be able to set it in
  `signals.env` without editing the baked unit; document-only would leave the
  unit silently ignoring buyer env and diverge from the container/Helm env
  contract. `EnvironmentFile=` still loads the file into the unit's environment
  so systemd fails cleanly if it is absent; `--env-file` is what actually
  crosses the container boundary.
- **R-AMI-06** (env secrets never logged — #292): The env-forwarding mechanism
  MUST NOT echo, `cat`, `tee`, or otherwise print the contents of `signals.env`
  or any token value to the journal / stdout / stderr. `--env-file` and
  `EnvironmentFile=` pass values into the process environment without rendering
  them on the command line or in logs; no bake or launch step may `cat`/`echo`
  the file, and no `set -x` may be enabled around a line carrying a token value.

## Invariants

- **INV-AMI-01** (no baked secrets): a bake produced from this component contains
  no credentials, token, or database config — only docker, the pinned image, and
  the systemd unit.
- **INV-AMI-02** (behavioral parity): a collector started from the baked AMI
  (given buyer-supplied config at launch) is the **same running collector** as
  the container/Helm deliveries — same image, same read-only enforcement and
  passwordless onboarding.
- **INV-AMI-03** (single EC2 contract): the component,
  `deploy/aws/terraform`, and `deploy/aws/cloudformation` install the collector
  the same way (docker-run the image), differing only in bake-time vs
  launch-time. This parity extends to env forwarding (all forward the
  buyer-supplied `signals.env` via `--env-file /etc/signals/signals.env`,
  R-AMI-05) **and to container networking (all run with `--network host`,
  R-AMI-07)**.
- **INV-AMI-04** (env secrets never leak to logs — #292): no artifact in either
  EC2 path prints the contents of `signals.env` or a token value to any log
  stream. A buyer's `SIGNALS_API_TOKEN` (or any other `SIGNALS_*` secret) reaches
  the collector only through the process environment, never through stdout /
  stderr / the systemd journal (R-AMI-06).

## Failure Conditions

- **FC-AMI-01**: The component YAML is not valid YAML, or is missing a required
  top-level key (`name`, `schemaVersion`, `phases`) → component lint fails.
- **FC-AMI-02**: `SignalsImage` default is `latest` or untagged → violates
  R-AMI-02; caught in review/lint.
- **FC-AMI-03**: The component writes a credential/config/token value into the
  image (e.g. a hardcoded token or a `signals.yaml` with a password) → violates
  R-AMI-01 / INV-AMI-01.
- **FC-AMI-04**: The unit forwards only `SIGNALS_API_TOKEN` (not the whole
  `signals.env`), so a buyer-supplied `SIGNALS_*` var is silently ignored →
  violates R-AMI-05 (the #292 defect).
- **FC-AMI-05**: A step `cat`/`echo`/`tee`s `signals.env` or a token value to a
  log stream → violates R-AMI-06 / INV-AMI-04.
- **FC-AMI-06** (#488): An EC2 run path runs the collector on the Docker bridge
  (no `--network host`) and/or publishes the API with `-p`. On a jumbo-frame
  (MTU 9001) instance the collector's TLS handshake to RDS black-holes and
  collection hangs/times out (`query role attributes: context deadline
  exceeded`) → violates R-AMI-07 / INV-AMI-03.

## Constraints

- AWSTOE component `schemaVersion: 1.0`; phases limited to `build`, `validate`,
  `test`.
- The AMI base is an Amazon Linux 2023 family image (matches the `dnf`-based
  docker install in `deploy/aws/terraform`).

## Out of scope

- **Container-image / EKS-add-on delivery** — #234 (shipped), #236 (shipped).
- **A native (non-container) collector binary install.** The EC2 path runs the
  container; the AMI does the same. A native-binary AMI is a separate future
  decision if demanded.
- **Live AMI build + `AmiProduct@1.0` standup / versioning** — governed by
  `specifications/marketplace-ami-product.md` (#235 gate opened 2026-07-19).

## Traceability

specification (this file) -> acceptance cases
(`marketplace-ami-image-builder.acceptance.md`) ->
`deploy/aws/imagebuilder/signals-collector-component.yaml` +
`deploy/aws/imagebuilder/README.md`.

The statically-checkable cases **TC-AMI-01..04** are enforced in CI by
`scripts/check-imagebuilder-component.sh` (wired into `scripts/preflight.sh`
as the `imagebuilder` gate and into `.github/workflows/ci.yml`), so the
component groundwork cannot regress (#266). **TC-AMI-04** guards that any
committed AMI change-set template is fully `${...}`-parameterized and safe
(no hardcoded `ami-*` id, seller account, or secret) — a correctness guard,
updated from its earlier form once the #235 gate opened (#466). **TC-AMI-06** and **TC-AMI-07**
(#292: env passthrough parity + secrets-never-logged) are enforced by
`tests/signals_ami_env_forwarding_test.go` (run under `go test`, the repo's
`test` gate). **TC-AMI-05** (live baked-AMI smoke) remains deferred until #235's
demand gate opens.
