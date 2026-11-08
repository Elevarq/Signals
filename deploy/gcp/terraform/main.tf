# GCP passwordless onboarding for Elevarq Signals (#113).
#
# Provisions a dedicated service account for the collector with
# roles/cloudsql.instanceUser, optionally creates the IAM DB user, and runs
# the collector on a GCE VM (with the SA attached) pre-wired for
# auth_method: gcp_cloudsql_iam over verify-full TLS on the direct libpq path.
# The credential is a short-lived Google OAuth2 token minted from the attached
# service account at connect time — no password is stored anywhere
# (INV001/INV002).
#
# The GRANT pg_monitor to the IAM user is a one-time SQL step; see ../README.md.

locals {
  labels = merge({ "app_kubernetes_io_name" = "signals", "managed-by" = "terraform" }, var.labels)

  # IAM DB user / PG role name = SA email without the .gserviceaccount.com
  # suffix (Google's documented truncation).
  pg_user = trimsuffix(google_service_account.collector.email, ".gserviceaccount.com")

  impersonate_yaml = var.gcp_impersonate_service_account == "" ? "" : "\n        gcp_impersonate_service_account: ${var.gcp_impersonate_service_account}"

  startup_script = <<-EOT
    #!/bin/bash
    set -euo pipefail
    apt-get update -y
    apt-get install -y docker.io openssl
    systemctl enable --now docker
    mkdir -p /etc/signals
    # --- Durable /data disk (#501, R-DUR-01..R-DUR-04) ------------------------
    # /data (signals.db: identity, history, cadence watermarks) is backed by the
    # separate persistent disk (google_compute_disk below: prevent_destroy; PDs
    # are encrypted at rest by default) attached via the attached_disk block at a
    # stable device_name — NOT the boot disk and NOT a Docker named volume. GCP
    # exposes an attached PD at /dev/disk/by-id/google-<device_name>. Mount
    # format-if-empty so a re-attached populated disk keeps its signals.db
    # untouched (R-DUR-03).
    # #713 (F07): resolve the intended persistent disk by its STABLE
    # by-id/google-<device_name> path, wait bounded for it, and format only if
    # provably blank — never scan-and-pick an arbitrary google-* disk, and never
    # treat a blkid probe error as "empty". Shared, tested logic in
    # deploy/common/scripts/prepare-data-disk.sh, embedded verbatim below.
    export DATA_DISK_IDENTITY="/dev/disk/by-id/google-${var.data_device_name}"
    export DATA_MOUNT="/data"
    export DATA_OWNER="10001:10001"
${indent(4, file("${path.module}/../../common/scripts/prepare-data-disk.sh"))}
    # Cloud SQL instance server CA for sslmode=verify-full (public certificate).
    cat > /etc/signals/cloudsql-ca.pem <<'PEM'
    ${var.db_server_ca_cert}
    PEM
    cat > /etc/signals/signals.yaml <<'YAML'
    env: ${var.env}
    signals:
      poll_interval: ${var.poll_interval}
    database:
      path: /data/signals.db
      wal: true
    api:
      # #719 (F13): bind the daemon to the CONTAINER interface (0.0.0.0) inside
      # its isolated network namespace, NOT the container's loopback. With Docker
      # bridge networking the host publish `-p 127.0.0.1:8081:8081` below delivers
      # to the container's eth0, so a 127.0.0.1-inside bind is unreachable from the
      # host. External exposure is still prevented by the host publish being
      # restricted to loopback (plus bearer-token auth), so this does not widen
      # reachability beyond the VM itself.
      listen_addr: "0.0.0.0:8081"
    targets:
      - name: ${var.db_name}-cloudsql
        host: ${var.db_host}
        port: ${var.db_port}
        dbname: ${var.db_name}
        user: "${local.pg_user}"
        auth_method: gcp_cloudsql_iam${local.impersonate_yaml}
        sslmode: verify-full
        sslrootcert_file: /etc/signals/cloudsql-ca.pem
    YAML
    # The config carries no secret; make the bind-mounted files world-readable
    # so the non-root container (uid 10001) can read them.
    chmod 0644 /etc/signals/signals.yaml /etc/signals/cloudsql-ca.pem
    # Mint-or-reuse the operator token and reconcile the collector container
    # idempotently (#732 / A7b): on a reboot or startup-script re-run, reuse the
    # already-minted token and reconcile the EXISTING container by name rather
    # than unconditionally `docker run` (which would fail on the name clash after
    # replacing the token, orphaning operator access). The reconcile logic lives
    # in deploy/gcp/scripts/reconcile-collector.sh and is embedded verbatim
    # below, so that script is the single, tested source of truth.
    # ENTRYPOINT is `tini --` with CMD `signals`; the `signals` arg in the script
    # is required, else the image args replace CMD and tini execs --config.
    # /data is the durable persistent-disk mount prepared above (#501): bind the
    # HOST path, never a Docker named volume (which lives on the ephemeral boot disk).
    export IMAGE_URI="${var.image_uri}"
    export TOKEN_FILE="/root/signals-api-token"
${indent(4, file("${path.module}/../scripts/reconcile-collector.sh"))}
  EOT
}

# --- Durable data disk (#501) -----------------------------------------------
# A separate persistent disk holds /data (signals.db). Its lifecycle is
# independent of the instance: prevent_destroy retains it, and the replacement
# instance re-attaches it at the same device_name, so collection history,
# instance identity, and cadence watermarks survive an instance/image upgrade
# (INV-DUR-01/02/05, R-DUR-05). Persistent disks are encrypted at rest by
# default (R-DUR-06).
resource "google_compute_disk" "data" {
  project = var.project_id
  name    = "${var.name_prefix}-data"
  zone    = var.zone
  type    = "pd-balanced"
  size    = var.data_disk_size
  labels  = local.labels

  lifecycle {
    # Replacing the instance must never destroy the store.
    prevent_destroy = true
  }
}

# --- Service account: the passwordless enabler ------------------------------

resource "google_service_account" "collector" {
  project      = var.project_id
  account_id   = "${var.name_prefix}-collector"
  display_name = "Elevarq Signals collector (passwordless Cloud SQL IAM)"
}

# Minimal role to authenticate to Cloud SQL as an IAM user.
resource "google_project_iam_member" "instance_user" {
  project = var.project_id
  role    = "roles/cloudsql.instanceUser"
  member  = "serviceAccount:${google_service_account.collector.email}"
}

# Optional: create the IAM DB user declaratively when the instance is known.
resource "google_sql_user" "collector" {
  count    = var.instance_name == "" ? 0 : 1
  project  = var.project_id
  name     = local.pg_user
  instance = var.instance_name
  type     = "CLOUD_IAM_SERVICE_ACCOUNT"
}

# --- Collector compute ------------------------------------------------------

resource "google_compute_instance" "collector" {
  project      = var.project_id
  name         = "${var.name_prefix}-collector"
  machine_type = var.machine_type
  zone         = var.zone
  labels       = local.labels

  boot_disk {
    initialize_params {
      image = var.image
    }
  }

  # #501: attach the durable data disk at a stable device_name. The boot-time
  # startup-script mounts /dev/disk/by-id/google-<device_name> at /data
  # format-if-empty. The disk is a separate resource (prevent_destroy), so
  # replacing this instance re-attaches the same disk — /data (signals.db)
  # survives the upgrade (R-DUR-01/R-DUR-02/R-DUR-05).
  attached_disk {
    source      = google_compute_disk.data.id
    device_name = var.data_device_name
    mode        = "READ_WRITE"
  }

  network_interface {
    network    = var.network
    subnetwork = var.subnetwork
    # No access_config block => no public IP; reach Cloud SQL over private IP.
  }

  service_account {
    email  = google_service_account.collector.email
    scopes = ["cloud-platform"]
  }

  metadata = {
    startup-script         = local.startup_script
    enable-oslogin         = "TRUE"
    block-project-ssh-keys = "TRUE"
  }

  shielded_instance_config {
    enable_secure_boot          = true
    enable_vtpm                 = true
    enable_integrity_monitoring = true
  }
}
