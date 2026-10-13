# Upgrading Elevarq Signals

Tracking: [#505](https://github.com/Elevarq/Signals/issues/505) (part of the
upgrade-lifecycle umbrella [#500](https://github.com/Elevarq/Signals/issues/500)).
Related: [`install/kubernetes-production.md`](install/kubernetes-production.md),
[`release-verification.md`](release-verification.md),
[`../deploy/aws/README.md`](../deploy/aws/README.md).

This is the operator-facing procedure for upgrading a **running** Signals
collector to a newer release **without losing its collection state**. It covers
every supported deploy mode and the one policy that governs all of them.

## What an upgrade must preserve

Signals is **stateful**. The container/image is disposable; the store at
**`/data/signals.db`** is not. That single SQLite store holds:

- the **instance identity** (`meta.instance_id`) — stable across upgrades;
- the **collection history** (`snapshots`, `query_runs`, `query_results`);
- the **cadence / incremental watermarks** that let a restarted collector
  resume *incrementally* instead of re-collecting from zero;
- circuit-breaker state and the event audit log.

An upgrade is correct only when, afterwards, the collector reports the **same
`instance_id`**, the **history and watermarks are intact**, and the **next
collection continues strictly after the last watermark** (not a from-zero
restart). Every per-mode procedure below ends with exactly that verification.

Credentials are **never** in `/data` — they are read at connect time from the
external config/secret store and never written to the store (so they are
unaffected by, and irrelevant to, the store-preservation steps).

## The upgrade policy (applies to every mode)

This policy is already implemented and tested — see the specs
[`store-schema-version.md`](../specifications/store-schema-version.md) (#502)
and [`cloud-data-durability.md`](../specifications/cloud-data-durability.md)
(#501), and the populated-upgrade regression gate
[`populated-upgrade-gate.md`](../specifications/populated-upgrade-gate.md)
(#504).

- **Migrations are forward-only.** On startup the binary runs
  `internal/db.Migrate()`, which applies — in order, each in its own
  transaction — the embedded migrations the store is missing, recording each
  in the `schema_migrations` ledger. Re-running the same version against an
  already-migrated store is a no-op. There are **no down-migrations**.
- **A newer store fails closed.** If the store's ledger records a migration the
  running binary does not ship (i.e. an **older** binary opened a store already
  migrated by a **newer** release), `Migrate()` refuses to run and returns a
  typed `*SchemaNewerError` (matchable with `errors.Is(err, ErrSchemaNewer)`).
  The refusal is **non-destructive**: nothing is applied, read for normal use,
  written, truncated, recreated, or discarded — the store is left exactly as
  found. The error names the store's recorded max migration, the binary's max
  known migration, and the recovery options.
- **A "downgrade" is a restore from backup, never a reverse migration.** If you
  must run an older binary against a store a newer binary already migrated, the
  supported recovery is to **restore the pre-upgrade backup** of
  `/data/signals.db` (or the durable data-volume snapshot) and run the older
  binary against *that*. Do not attempt to hand-edit the schema or the ledger.
- **Therefore: back up `/data` before every upgrade.** The backup is the
  recovery mechanism for all modes. Each procedure below states the backup step
  first.

> This is the **producer-side** policy for the store. It is distinct from the
> snapshot/output-format compatibility policy (how long a *snapshot* version is
> emitted and what a downstream consumer may rely on), which is documented
> separately — see
> [snapshot/output-format compatibility & deprecation policy (#503)](https://github.com/Elevarq/Signals/issues/503).

## Mode matrix

| Mode | Durable store | Backup of record | Covered in |
|---|---|---|---|
| Container / Docker Compose | Docker **named volume** `signals-data` → `/data` | copy `signals.db` while stopped | [below](#container--docker-compose) |
| Helm / EKS | **PVC** → `/data` (retain-policy storage class) | PVC volume-snapshot | [k8s runbook](install/kubernetes-production.md#upgrade-behavior) + [below](#helm--eks) |
| AWS / Azure / GCP single-instance | **separate durable block volume** (EBS / managed disk / PD) → `/data` | provider volume snapshot | [AWS README](../deploy/aws/README.md#data-durability-and-upgrades-501) + [below](#aws--azure--gcp-single-instance) |
| Air-gapped / offline | whichever of the above the install uses | same as the underlying mode | [below](#air-gapped--offline) |

The eval-only non-persistent modes are **out of scope for upgrade**: Helm
`persistence.enabled: false` (emptyDir) and a container run with **no** `-v`
mount lose the entire store on restart. Those are fenced off as evaluation-only
by the umbrella (#500) and must not be used where collection history matters.

---

## Container / Docker Compose

The store lives on a Docker **named volume** (`signals-data`, mounted at
`/data`). A named volume **persists across container recreation**, so pulling a
new image and recreating the container keeps the store — provided the volume is
a real named volume on persistent host storage (not a throwaway mount, and not
the ephemeral root disk of a VM that is itself being replaced — for that case
see the [single-instance section](#aws--azure--gcp-single-instance)).

### Backup

Quiesce the store, then copy it:

```sh
docker stop signals

# Copy the whole /data (signals.db plus any -wal/-shm companion files)
# out of the named volume to a host path.
docker run --rm \
  -v signals-data:/src -v "$PWD/signals-backup:/dst" \
  alpine sh -c 'cp -a /src/. /dst/'
```

Verify the backup before continuing:

```sh
sqlite3 signals-backup/signals.db \
  'select value from meta where key="instance_id"; select count(*) from snapshots;'
```

You should see the expected `instance_id` and a non-zero snapshot count.

### Upgrade

Pin the new version and recreate the container against the **same** volume.

Compose:

```sh
# Edit the image tag in your compose file to the new version, then:
docker compose pull
docker compose up -d        # recreates the container, re-mounts signals-data
```

Plain `docker run`: `docker stop signals && docker rm signals`, then re-run the
new image with the **same** `-v signals-data:/data`.

On startup the new binary runs `internal/db.Migrate()` and applies any missing
forward migrations in place.

### Post-upgrade verification

```sh
TOKEN=...   # your API bearer token
docker exec -e SIGNALS_API_TOKEN="$TOKEN" signals signalsctl status
```

Confirm:

- **same `instance_id`** as before the upgrade;
- history is intact (snapshot count ≥ the pre-upgrade count);
- the **next** collection cycle continues incrementally from the watermark
  (watch the logs — it is not a from-zero re-collect).

### Recovery

If the new version crash-loops or refuses to start (e.g. you rolled *back* to an
older image and it logs a `*SchemaNewerError`), restore the backup and run the
version that matches it:

```sh
docker stop signals
docker run --rm \
  -v signals-data:/dst -v "$PWD/signals-backup:/src" \
  alpine sh -c 'rm -rf /dst/* && cp -a /src/. /dst/ && chown -R 10001:10001 /dst'
# start the binary version that produced the backup
```

Forward-only: there is no reverse migration — restoring the pre-upgrade backup
*is* the downgrade.

---

## Helm / EKS

The store lives on a **PVC** mounted at `/data`. The production profile uses a
retain-policy storage class so the volume survives pod replacement and
`helm uninstall`. The full runbook — install profile, least-privilege posture,
rollback, and uninstall retention — is
[`install/kubernetes-production.md`](install/kubernetes-production.md); this
section is the upgrade-specific summary. Do not duplicate the runbook; extend
it.

### Backup

Take a volume snapshot of the PVC before upgrading (CSI `VolumeSnapshot`, or
your backup tool):

```sh
kubectl -n observability get pvc signals-signals-data
# create a VolumeSnapshot of that PVC via your cluster's snapshot class
```

### Upgrade

```sh
helm upgrade signals deploy/helm/signals/ \
  --namespace observability \
  --values production.yaml \
  --version <new-version>
```

The rollout schedules a new pod that re-mounts the **existing** PVC at
`/data/signals.db`; `internal/db.Migrate()` runs on startup and applies missing
forward migrations. The readiness probe (`/readyz`) only passes once the store
is reachable and the first cycle completes, so a migration failure surfaces as a
failed rollout rather than a silently broken collector.

### Post-upgrade verification

```sh
kubectl -n observability exec deploy/signals-signals -- \
  signalsctl status
```

Confirm the same `instance_id`, intact history/watermarks, and that the next
cycle continues incrementally. See the runbook's
[Upgrade behavior](install/kubernetes-production.md#upgrade-behavior) and
[Rollback](install/kubernetes-production.md#rollback) sections for detail.

### Recovery

`helm rollback` reverts the Deployment but **not** the store schema (forward
migrations already ran). If the older binary cannot open the newer store it
fails closed with `*SchemaNewerError` and the pod crash-loops — the signal to
restore. Restore the pre-upgrade PVC snapshot (provision a PVC from the
snapshot, or restore in place with your backup tool) and roll back to the
matching chart revision. This is why the backup step precedes the upgrade.

---

## AWS / Azure / GCP single-instance

The VM/AMI deploys (Terraform, CloudFormation, Image Builder, Bicep) back
`/data` with a **separate, encrypted, durable block volume** whose lifecycle is
independent of the compute instance:

| Provider | Durable resource |
|---|---|
| AWS | separate encrypted EBS volume (`aws_ebs_volume.data` / CFN `DataVolume`), `prevent_destroy` / `Retain` policies |
| Azure | `azurerm_managed_disk` / Bicep data disk at a fixed LUN, `Detach` on delete |
| GCP | `google_compute_disk` attached at a stable device name, `prevent_destroy` |

Instance/AMI replacement replaces **only the compute instance**; the data volume
is retained and **re-attached** to the replacement, and the boot-time mount is
**format-if-empty** (a populated volume keeps its `signals.db` untouched, a fresh
volume is formatted exactly once and chowned to uid 10001). This is the fix for
the earlier **root-disk pitfall**, where `/data` lived on a Docker named volume
on the ephemeral root disk and was destroyed on every instance replacement.

The AWS procedure — including the one-time **migration off a legacy root-disk
install** — is documented in full in
[`../deploy/aws/README.md`](../deploy/aws/README.md#data-durability-and-upgrades-501).
Azure and GCP follow the same shape against their respective durable disks.

### Backup

Snapshot the **durable data volume** (not the root disk):

- AWS: `aws ec2 create-snapshot --volume-id <data-ebs-vol>`
- Azure: snapshot the managed data disk
- GCP: `gcloud compute disks snapshot <data-disk>`

Provider snapshots are independent of the instance and are the backup of record
for the upgrade moment.

### Upgrade

Replace the compute instance via the module's normal path — Terraform
`user_data_replace_on_change` (compute only), a CloudFormation replacement stack
update, or a new AMI. The durable volume is retained and re-attached; the new
instance's collector runs `internal/db.Migrate()` against the preserved store.

> **Do not** let a new instance initialize an empty store over an existing one.
> Durability is guaranteed only for the **separate durable volume**; a legacy
> root-disk install must first be migrated by hand (stop collector → copy the
> live store off → verify identity+history → re-apply → copy onto the durable
> volume with the collector stopped → re-verify → start). See the AWS README's
> [Transitioning an existing root-disk install](../deploy/aws/README.md#transitioning-an-existing-root-disk-install-migration)
> section.

### Post-upgrade verification

Over SSM Session Manager (AWS) or the equivalent, confirm the store on the
re-attached volume:

```sh
sqlite3 /data/signals.db \
  'select value from meta where key="instance_id"; select count(*) from snapshots;'
docker exec -e SIGNALS_API_TOKEN="$(sudo cat /root/signals-api-token)" \
  signals signalsctl status
```

Same `instance_id`, intact history, and the next cycle continuing incrementally.

### Recovery

Restore the pre-upgrade volume snapshot to a new volume and attach it in place
of the current data volume. Forward-only applies: there is no reverse migration;
the snapshot restore is the downgrade. A pre-fix install that already lost
`/data` on a prior root-disk replacement **cannot** recover that history — the
first apply of the durable-volume fix seeds a fresh store and collection
restarts from that point.

---

## Air-gapped / offline

Air-gapped upgrades use the **same store-preservation and migration mechanics**
as the underlying mode (container, Helm, or single-instance) — the only
difference is that the new image arrives and is verified **offline**. The store
never leaves the environment and is preserved exactly as above.

### 1. Transfer and verify the signed image offline

On a connected workstation, pull the pinned version **by digest**, verify its
signature and SBOM, then export it to a file for transfer:

```sh
# Verify the cosign keyless signature and SBOM before trusting the image.
# Full checklist: docs/release-verification.md
cosign verify ghcr.io/elevarq/signals:<version> \
  --certificate-identity-regexp='github.com/Elevarq/(Signals|signals)/.github/workflows/release.yml@' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com'

cosign verify-attestation --type spdxjson \
  --certificate-identity-regexp='github.com/Elevarq/(Signals|signals)/.github/workflows/release.yml@' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com' \
  ghcr.io/elevarq/signals:<version>

# Save the exact digest-pinned image for offline transfer.
docker save ghcr.io/elevarq/signals@sha256:<digest> -o signals-<version>.tar
```

Carry `signals-<version>.tar` across the air gap on approved media. Inside the
enclave, **re-verify** before loading — either carry the cosign bundle and run
`cosign verify` offline (COSIGN_EXPERIMENTAL / local key/bundle), or at minimum
verify the digest matches what you exported:

```sh
docker load -i signals-<version>.tar
docker inspect --format '{{index .RepoDigests 0}}' <image-id>   # must match sha256:<digest>
```

Load the image into your in-enclave registry (or the node's local daemon) under
the tag your deploy mode references. The offline-import + signature-verification
step is the air-gap-specific part; everything after it is identical to the
underlying mode.

### 2. Back up `/data` (offline)

Same as the underlying mode — copy `signals.db` (container), snapshot the PVC
(Helm), or snapshot the durable volume (single-instance). No network is needed;
the backup stays inside the enclave.

### 3. Upgrade and migrate forward (offline)

Recreate the container / roll the Deployment / replace the instance against the
loaded image. Migrations are **embedded in the binary** — `internal/db.Migrate()`
applies them entirely offline, with no download, no external schema source, and
no network call. The forward-only / fail-closed policy holds unchanged.

### 4. Verify (offline)

Identical to the underlying mode: same `instance_id`, intact history and
watermarks, next collection continues incrementally. `signalsctl doctor --json`
produces a deterministic, secret-free evidence artifact suitable for an
air-gapped audit trail.

### Recovery

Restore the offline backup from step 2 and run the matching binary version.
Forward-only: the restore is the downgrade.

---

## See also

- [`install/kubernetes-production.md`](install/kubernetes-production.md) — full Helm/EKS runbook
- [`../deploy/aws/README.md`](../deploy/aws/README.md) — AWS durable-volume + root-disk migration detail
- [`release-verification.md`](release-verification.md) — full signature / SBOM / provenance checklist
- [snapshot/output-format compatibility & deprecation policy (#503)](https://github.com/Elevarq/Signals/issues/503) — producer-side format policy
- [`../specifications/store-schema-version.md`](../specifications/store-schema-version.md) — forward-only / fail-closed schema policy (#502)
- [`../specifications/cloud-data-durability.md`](../specifications/cloud-data-durability.md) — `/data` durability guarantees (#501)
- [`../specifications/populated-upgrade-gate.md`](../specifications/populated-upgrade-gate.md) — populated-upgrade regression gate (#504)
</content>
</invoke>
