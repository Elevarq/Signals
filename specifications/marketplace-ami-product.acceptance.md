# Acceptance Tests: Marketplace AMI Product (live AmiProduct@1.0 standup)

## Feature

`specifications/marketplace-ami-product.md`

## Test Cases

### TC-AMIP-01: AMI baked only from the committed component + pinned image (invariant)

**Rule:** INV-AMIP-01 / FC-AMIP-01 / R-AMIP-02

**Scenario:** The listed AMI must be traceable to the reviewed component + a
pinned released image, not an out-of-band hand-built AMI.

**Given:**
- The EC2 Image Builder image recipe and the `create-component` at the released
  semantic version, whose `data` equals
  `deploy/aws/imagebuilder/signals-collector-component.yaml`.

**When:**
- The recipe's components and the component's `SignalsImage` are inspected.

**Then:**
- The recipe uses the `signals-collector` component (byte-identical `data`) on an
  Amazon Linux 2023 base and no divergent inline packages.
- `SignalsImage` is a pinned tag (no `latest`), matching the released version.

---

### TC-AMIP-02: AccessRole is least-privilege (invariant)

**Rule:** R-AMIP-04 / FC-AMIP-02

**Scenario:** Guard against an over-broad role AWS Marketplace assumes to
scan/copy the AMI.

**Given:**
- The `AccessRoleArn` policy document and trust policy.

**When:**
- The attached policy is inspected.

**Then:**
- The policy grants only the AWS-documented AMI-product access (AMI
  scan/copy — e.g. the `AWSMarketplaceAmiIngestion`-scoped actions), scoped to
  the listed AMI/snapshot resources.
- The trust policy is scoped to the AWS Marketplace service principal.
- No `ec2:*`, no wildcard resource.

---

### TC-AMIP-03: Change-set copy is ASCII-only and fully substituted (failure)

**Rule:** R-AMIP-05 / FC-AMIP-03

**Scenario:** A standup run leaves a `${...}` unsubstituted or pastes smart
punctuation into the description.

**Given:**
- The AMI product change-set(s) rendered by `scripts/marketplace-changeset.sh`.

**When:**
- The render + guards run.

**Then:**
- The script exits non-zero before `start-change-set` on any leftover `${...}`
  or non-ASCII byte.
- `VersionTitle`/advertised version is the real released SemVer (no literal
  `${VERSION}`).

---

### TC-AMIP-04: Separate product, container product untouched (invariant)

**Rule:** R-AMIP-01

**Scenario:** The AMI standup must not modify or bundle with the container
product.

**Given:**
- The `CreateProduct` change-set with `Type: AmiProduct@1.0`.

**When:**
- The change-set is inspected.

**Then:**
- It creates a new `AmiProduct@1.0` entity, not a delivery option or edit on
  `prod-7tz6zxncwjmw4`.
- No change references the container product id.

---

### TC-AMIP-05: Live baked-AMI parity smoke (deferred — gated)

**Rule:** INV-AMIP-02

**Scenario:** A buyer bakes an AMI with the shared component and boots it with
launch-supplied config.

**Given:**
- An AMI baked from an Amazon Linux 2023 recipe + the `signals-collector`
  component, launched with `/etc/signals` config supplied at launch.

**When:**
- The instance boots and `signals.service` starts.

**Then:**
- The collector runs (same image, read-only, passwordless onboarding) and
  produces a snapshot equivalent to the container/Helm deliveries.
- No credentials are present in the AMI itself (INV-AMI-01).

This is a live, gated smoke (real AMI bake + launch); run when the standup
reaches representative-environment validation.
