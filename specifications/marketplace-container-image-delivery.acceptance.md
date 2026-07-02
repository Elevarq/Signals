# Acceptance Tests: Marketplace Container-Image Delivery Option

## Feature

`specifications/marketplace-container-image-delivery.md`

## Test Cases

### TC-CID-01: Rendered change-set adds a valid ECR delivery option (normal)

**Rule:** Normal — happy path (R-CID-01, R-CID-02)

**Scenario:** Release engineer renders the container-image change-set for the
released version.

**Given:**
- `04-add-container-image-delivery.json` with `${VERSION}`, `${IMAGE_URI}`,
  `${USAGE_INSTRUCTIONS}` supplied.

**When:**
- The template is rendered by `scripts/marketplace-changeset.sh`.

**Then:**
- The rendered document is valid JSON.
- It contains exactly one `EcrDeliveryOptionDetails` delivery option.
- `CompatibleServices` == `["ECS"]`.
- The change-set does NOT modify or remove the existing Helm delivery option
  (the template's `ChangeType` is `AddDeliveryOptions`, additive).

---

### TC-CID-02: Container image is identical to the Helm option's image (invariant)

**Rule:** Invariant — INV-CID-01

**Scenario:** Guard against the two delivery options drifting to different
images.

**Given:**
- The rendered container-image change-set and the live/rendered Helm delivery
  change-set for the same `${VERSION}`.

**When:**
- The `ContainerImages` entry of each is compared.

**Then:**
- Both reference the same Marketplace-ECR repository and tag.
- The resolved image digest is identical (one artifact, two delivery options).

---

### TC-CID-03: Usage instructions carry the non-Helm deployment contract (boundary)

**Rule:** Boundary — R-CID-03, 4000-char limit

**Scenario:** A buyer with no Kubernetes reads the usage instructions to deploy
on ECS/Fargate.

**Given:**
- The rendered `UsageInstructions` string.

**When:**
- Its content and length are inspected.

**Then:**
- Length is > 0 and <= 4000 characters.
- It contains an ECR login line using `--region us-east-1`.
- It documents: a writable volume mount for local snapshots, non-root execution,
  and config-file + secret injection.
- It warns that durable storage is required for snapshots (Amazon EFS on Fargate,
  or an EBS / bind volume on EC2-backed ECS), because the Fargate task filesystem
  is ephemeral.
- It is ASCII-only.

---

### TC-CID-04: Unsupported CompatibleServices value is rejected before submit (invalid)

**Rule:** Failure condition — FC-CID-03

**Scenario:** A typo puts an unsupported orchestrator in `CompatibleServices`.

**Given:**
- A rendered change-set whose `CompatibleServices` contains a value outside
  `{ECS, EKS, ECS-Anywhere, EKS-Anywhere, Bedrock-AgentCore}` (e.g. `GKE`).

**When:**
- Preflight validation runs before `start-change-set`.

**Then:**
- Validation fails with a message naming the offending value.
- `start-change-set` is not called.

---

### TC-CID-05: Standalone container produces the same collector as Helm (failure/live)

**Rule:** Invariant — INV-CID-02, INV-CID-03 (live-artifact smoke)

**Scenario:** The Marketplace-ECR image is run with no Kubernetes/Helm, the way
an ECS/Fargate buyer would, against a reachable representative RDS target.

**Given:**
- The Marketplace-ECR image at `${VERSION}` pulled with `--region us-east-1`.
- A minimal config + secret and a writable snapshot volume, run non-root
  (`docker run` / ECS task, no K8s).

**When:**
- The container starts and performs one collection cycle against the target.

**Then:**
- A read-only diagnostic snapshot is produced, equivalent in shape to the one
  the Helm-deployed collector produces for the same target.
- No writes are issued to the target (read-only preserved).
- The container runs as a non-root user.

---

### TC-CID-06: Unsubstituted placeholder / non-ASCII fails fast (failure)

**Rule:** Failure conditions — FC-CID-01, FC-CID-02

**Scenario:** A release run forgets to export a variable, or pastes smart
punctuation into `USAGE_INSTRUCTIONS`.

**Given:**
- The container-image template rendered with a missing `${...}` variable, OR
  with a non-ASCII character (em-dash, curly quote) in a rendered field.

**When:**
- `scripts/marketplace-changeset.sh` runs.

**Then:**
- The script exits non-zero before `start-change-set`.
- The unsubstituted-`${...}` guard (FC-CID-01) or the non-ASCII guard
  (FC-CID-02) reports the offending content.
