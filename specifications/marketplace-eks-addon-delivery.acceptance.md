# Acceptance Tests: Marketplace EKS Add-On Delivery Option

## Feature

`specifications/marketplace-eks-addon-delivery.md`

## Test Cases

### TC-EAO-01: Rendered change-set adds a valid EKS add-on option (normal)

**Rule:** Normal — happy path (R-EAO-01, R-EAO-02, R-EAO-03)

**Scenario:** Release engineer renders the EKS add-on change-set for the released
version.

**Given:**
- `05-add-eks-addon-delivery.json` with `${VERSION}`, `${IMAGE_URI}`,
  `${CHART_URI}`, `${K8S_VERSIONS}`, and the description/usage vars supplied.

**When:**
- The template is rendered by `scripts/marketplace-changeset.sh`.

**Then:**
- The rendered document is valid JSON.
- It contains exactly one `EksAddOnDeliveryOptionDetails` delivery option.
- `AddOnName` == `signals`, `AddOnType` == `observability`, `Namespace` ==
  `signals`.
- `SupportedArchitectures` == `["amd64", "arm64"]`.
- `ChangeType` is `AddDeliveryOptions` (additive; Helm + container options
  untouched).

---

### TC-EAO-02: Add-on image and chart match the Helm option (invariant)

**Rule:** Invariant — INV-EAO-01

**Scenario:** Guard against the add-on referencing a different image/chart than
the live Helm delivery.

**Given:**
- The rendered add-on change-set and the Helm delivery change-set for the same
  `${VERSION}`.

**When:**
- `ContainerImages` and `HelmChartUri` of each are compared.

**Then:**
- The add-on's `ContainerImages` equals the Helm option's `ContainerImages`
  (same MP-ECR repo, tag, resolved digest).
- The add-on's `HelmChartUri` equals the Helm option's `HelmChartUri`.

---

### TC-EAO-03: Usage instructions carry the add-on install path + durability note (boundary)

**Rule:** Boundary — 4000-char limit, durable-storage note

**Scenario:** A buyer installs Signals from the EKS add-on catalog.

**Given:**
- The rendered `UsageInstructions` string.

**When:**
- Its content and length are inspected.

**Then:**
- Length is > 0 and <= 4000 characters.
- It documents the EKS add-on install path (console or `aws eks create-addon` /
  `eksctl`).
- It states that the collector's local snapshot store needs a PersistentVolume.
- It is ASCII-only.

---

### TC-EAO-04: Unsupported AddOnType is rejected before submit (invalid)

**Rule:** Failure condition — FC-EAO-03

**Scenario:** A typo puts an unsupported add-on type in the template.

**Given:**
- A rendered change-set whose `AddOnType` is outside the AWS-valid set (e.g.
  `diagnostics`).

**When:**
- Preflight validation runs before `start-change-set`.

**Then:**
- Validation fails with a message naming the offending value.
- `start-change-set` is not called.

---

### TC-EAO-05: Add-on install produces the same collector as Helm (failure/live)

**Rule:** Invariant — INV-EAO-02 (live-artifact smoke)

**Scenario:** The add-on is installed on a representative EKS cluster via the
add-on flow, against a reachable RDS target.

**Given:**
- An EKS cluster on a `CompatibleKubernetesVersions` version.
- The Signals add-on installed via `aws eks create-addon` (not `helm install`)
  with a durable volume and a reachable target.

**When:**
- The add-on deploys and performs one collection cycle.

**Then:**
- A read-only diagnostic snapshot is produced, equivalent to the one the
  Helm-installed collector produces for the same target.
- No writes are issued to the target (read-only preserved).
- The collector runs in the `signals` namespace, non-root.

---

### TC-EAO-06: Unsubstituted placeholder / non-ASCII fails fast (failure)

**Rule:** Failure conditions — FC-EAO-01, FC-EAO-02

**Scenario:** A release run forgets a variable, or pastes smart punctuation into
`ADDON_USAGE_INSTRUCTIONS`.

**Given:**
- The add-on template rendered with a missing `${...}` variable, OR with a
  non-ASCII character in a rendered field.

**When:**
- `scripts/marketplace-changeset.sh` runs.

**Then:**
- The script exits non-zero before `start-change-set`.
- The unsubstituted-`${...}` guard (FC-EAO-01) or the non-ASCII guard
  (FC-EAO-02) reports the offending content.

---

### TC-EAO-07: Chart values.schema.json exposes the add-on config contract (invariant)

**Rule:** R-EAO-07 / FC-EAO-05 / INV-EAO-02

**Scenario:** Guard against shipping an add-on that `aws eks
describe-addon-configuration` reports as "No configuration support", which leaves
a buyer unable to point the add-on at a database.

**Given:**
- `deploy/helm/signals/values.schema.json`.

**When:**
- The schema is parsed and its declared properties are inspected.

**Then:**
- `target` declares `host`, `user`, `authMethod`, `sslmode`, and
  `sslRootCertFile`.
- `persistence` declares `storageClass`.
- `serviceAccount` declares `annotations`.
- `extraEnv` is still declared (the chart-managed-name guard is preserved).
- `helm template` with the default values and with a representative buyer values
  file both still validate against the schema (no regression for existing Helm
  installs).
