# Acceptance Tests: Marketplace AMI / EC2 Image Builder Groundwork

## Feature

`specifications/marketplace-ami-image-builder.md`

## Test Cases

### TC-AMI-01: Component is a valid AWSTOE document (normal)

**Rule:** Normal — happy path

**Scenario:** CI / a reviewer validates the committed Image Builder component.

**Given:**
- `deploy/aws/imagebuilder/signals-collector-component.yaml`.

**When:**
- The file is parsed as YAML and its top-level keys are inspected.

**Then:**
- It is valid YAML.
- It has `name`, `description`, `schemaVersion: 1.0`, and a `phases` array.
- `phases` contains `build`, `validate`, and `test` phases.

---

### TC-AMI-02: No baked secrets or config (invariant)

**Rule:** Invariant — INV-AMI-01, R-AMI-01, R-AMI-03

**Scenario:** Guard that the component never bakes credentials or database config
into the AMI.

**Given:**
- The component YAML.

**When:**
- Its `build`-phase commands are scanned.

**Then:**
- No step writes a control-plane token value, database password, or a populated
  `signals.yaml` with connection credentials into the image.
- Config (`/etc/signals`) and the token are described as **launch-time**
  (user-data / SSM) inputs, not bake-time.

---

### TC-AMI-03: SignalsImage is pinned, not latest (boundary)

**Rule:** Boundary / failure — R-AMI-02, FC-AMI-02

**Scenario:** The image parameter must be reproducible.

**Given:**
- The `SignalsImage` parameter definition.

**When:**
- Its default value is inspected.

**Then:**
- The default is a pinned version tag (matches `:<major>.<minor>.<patch>`).
- It is not `latest` and not untagged.

---

### TC-AMI-04: Groundwork triggers no live Marketplace change-set (invalid/failure)

**Rule:** Failure condition — R-AMI-04

**Scenario:** The groundwork must not stand up the live AMI product.

**Given:**
- The files added by this slice (component YAML, README, spec).

**When:**
- They are inspected for any executable Marketplace `start-change-set` template
  for an `AmiProduct@1.0`.

**Then:**
- There is no runnable AMI-product change-set template committed — only the
  documented, demand-gated scaffolding in the README.

---

### TC-AMI-05: Baked AMI runs the same collector as the container delivery (failure/live)

**Rule:** Invariant — INV-AMI-02, INV-AMI-03 (live-artifact smoke, deferred)

**Scenario:** When demand justifies a live AMI, a bake from this component,
launched with buyer-supplied config against a reachable RDS target, runs the
collector.

**Given:**
- An AMI baked via this component on Amazon Linux 2023.
- Launch-time config + token + a reachable target supplied via user-data / SSM.

**When:**
- The instance boots and `signals.service` starts the container.

**Then:**
- A read-only diagnostic snapshot is produced, equivalent to the container /
  Helm delivery for the same target.
- No writes are issued to the target.
- The running image digest equals `SignalsImage`.

**Note:** This case is realized only if #235's demand gate opens; it is
documented here so the contract is defined, not run in the groundwork slice.

---

### TC-AMI-06: Buyer-supplied SIGNALS_* env reaches the collector, in parity (normal + invariant)

**Rule:** Normal / invariant — R-AMI-05, INV-AMI-03, FC-AMI-04 (#292)

**Scenario:** A buyer places an arbitrary `SIGNALS_*` variable (e.g. the dev-only
`SIGNALS_ALLOW_INSECURE_PG_TLS=true`, which surfaced this in the #235 AMI
launch-test) in `/etc/signals/signals.env`. It must reach the containerized
collector, and the AMI component and the terraform run path must forward env the
same way.

**Given:**
- `deploy/aws/imagebuilder/signals-collector-component.yaml`.
- `deploy/aws/terraform/main.tf`.
- `deploy/aws/cloudformation/signals-rds-iam.yaml`.

**When:**
- The `signals.service` unit's `docker run` line (component) and the terraform /
  cloudformation `docker run` invocations are inspected.

**Then:**
- All three docker-run the image with `--env-file /etc/signals/signals.env`,
  forwarding the whole file — not only `-e SIGNALS_API_TOKEN`.
- The component's `EnvironmentFile=/etc/signals/signals.env` is retained (systemd
  fails cleanly if absent).
- All three paths forward env identically (INV-AMI-03 parity).

---

### TC-AMI-07: The env-forwarding path never logs a secret (invariant / security)

**Rule:** Invariant — R-AMI-06, INV-AMI-04, FC-AMI-05 (#292)

**Scenario:** Env is passed to the container through the process environment,
never rendered to a log stream, so a buyer's `SIGNALS_API_TOKEN` value cannot
leak into the journal / stdout.

**Given:**
- The component YAML, `deploy/aws/terraform/main.tf`, and
  `deploy/aws/cloudformation/signals-rds-iam.yaml`.

**When:**
- Every line that references `signals.env` or a token value is inspected.

**Then:**
- No step `cat`s, `echo`s, `tee`s, or otherwise prints the contents of
  `signals.env`.
- No `-e SIGNALS_API_TOKEN=<value>` or token value appears on a docker command
  line (env is passed by reference via `--env-file` / `-e NAME` only).
- No `set -x` is enabled around a line carrying a token value.
