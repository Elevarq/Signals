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
    apt-get install -y docker.io
    systemctl enable --now docker
    mkdir -p /etc/signals
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
      listen_addr: "127.0.0.1:8081"
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
    docker run -d --name signals --restart=always \
      -v /etc/signals:/etc/signals:ro \
      -v signals-data:/data \
      -p 127.0.0.1:8081:8081 \
      ${var.image_uri} --config /etc/signals/signals.yaml
  EOT
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
