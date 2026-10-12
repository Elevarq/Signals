# AWS passwordless onboarding for Elevarq Signals (#111).
#
# Provisions the collector's IAM identity (instance profile) with a minimal
# rds-db:connect policy scoped to one DB user, and runs the collector on EC2
# pre-wired for auth_method: aws_rds_iam over verify-full TLS. The credential
# is a short-lived RDS IAM token minted from this identity at connect time —
# no password is stored anywhere (INV001/INV002).
#
# The DB-side grant (GRANT rds_iam / GRANT pg_monitor) is a one-time SQL step;
# see ../README.md.

data "aws_caller_identity" "current" {}

# Amazon Linux 2023 (x86_64) latest, via SSM public parameter.
data "aws_ssm_parameter" "al2023" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

locals {
  tags = merge({ "app.kubernetes.io/name" = "signals", "managed-by" = "terraform" }, var.tags)

  # rds-db:connect resource ARN for exactly this DB user on this instance.
  rds_connect_arn = "arn:aws:rds-db:${var.region}:${data.aws_caller_identity.current.account_id}:dbuser:${var.db_resource_id}/${var.db_user}"

  user_data = <<-EOT
    #!/bin/bash
    set -euo pipefail
    dnf install -y docker
    systemctl enable --now docker
    # AL2023 ships the SSM agent; ensure it is running so the operator can
    # reach the instance via Session Manager / Run Command (no SSH key pair).
    systemctl enable --now amazon-ssm-agent || true
    mkdir -p /etc/signals
    # --- Durable /data volume (#501, R-DUR-01..R-DUR-04) ----------------------
    # The collector's entire store (signals.db: instance identity, collection
    # history, cadence watermarks) lives at /data. It MUST survive instance/AMI
    # replacement, so /data is backed by a SEPARATE EBS volume (aws_ebs_volume
    # below: prevent_destroy + encrypted) attached at a stable device — NOT the
    # root disk and NOT a Docker named volume on the root disk. Mount it
    # format-if-empty: a fresh volume is formatted once; a re-attached populated
    # volume keeps its signals.db untouched (R-DUR-03).
    DATA_DEV="${var.data_device_name}"
    # NVMe-backed instances remap /dev/sdX to /dev/nvmeXn1, so resolve the real
    # device: the single non-root block device that is not the OS disk.
    if [ ! -b "$DATA_DEV" ]; then
      for d in /dev/nvme*n1; do
        [ -b "$d" ] || continue
        # Skip the root device (mounted at /).
        if lsblk -no MOUNTPOINT "$d" | grep -q '^/$'; then continue; fi
        DATA_DEV="$d"
      done
    fi
    mkdir -p /data
    # Format ONLY when the device has no filesystem (first provisioning).
    if ! blkid "$DATA_DEV" >/dev/null 2>&1; then
      mkfs.ext4 -F "$DATA_DEV"
    fi
    # Persist the mount across reboots by stable UUID (survives device renames).
    DATA_UUID="$(blkid -s UUID -o value "$DATA_DEV")"
    grep -q "$DATA_UUID" /etc/fstab || \
      echo "UUID=$DATA_UUID /data ext4 defaults,nofail 0 2" >> /etc/fstab
    mount /data || mount "$DATA_DEV" /data
    # The non-root container user (uid 10001, group 10001) must own /data so it
    # can write signals.db (R-DUR-04). On a re-attached populated volume this is
    # a cheap no-op that preserves the existing data.
    chown 10001:10001 /data
    # RDS server CA bundle for sslmode=verify-full.
    curl -fsSL https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem \
      -o /etc/signals/rds-ca.pem
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
      - name: ${var.db_name}-rds
        host: ${var.db_host}
        port: ${var.db_port}
        dbname: ${var.db_name}
        user: ${var.db_user}
        auth_method: aws_rds_iam
        region: ${var.region}
        sslmode: verify-full
        sslrootcert_file: /etc/signals/rds-ca.pem
    YAML
    # The config carries no secret; make the bind-mounted files world-readable
    # so the non-root container (uid 10001) can read them.
    chmod 0644 /etc/signals/signals.yaml /etc/signals/rds-ca.pem
    # Mint a strong control-plane API token and write it to the same
    # /etc/signals/signals.env the AMI unit uses (#292 parity, INV-AMI-03), so
    # the whole file — this token plus any buyer-added SIGNALS_* — is forwarded
    # to the container by reference via `--env-file`. Root-only (0600): the
    # docker client reads it as root at run time, so the container does not need
    # to. The token value is NEVER put on the docker command line (it would be
    # visible in the journal / `ps`) — R-AMI-06 / INV-AMI-04.
    umask 077
    printf 'SIGNALS_API_TOKEN=%s\n' "$(openssl rand -hex 32)" > /etc/signals/signals.env
    chmod 0600 /etc/signals/signals.env
    # Keep the operator-readable copy of just the token for the verify step.
    sed -n 's/^SIGNALS_API_TOKEN=//p' /etc/signals/signals.env > /root/signals-api-token
    chmod 0600 /root/signals-api-token
    # ENTRYPOINT is `tini --` with CMD `signals`; the `signals` arg below is
    # required, else the image args replace CMD and tini execs --config.
    # --network host: use the instance ENI directly (R-AMI-07). On jumbo-frame
    # ENIs (MTU 9001, the in-VPC default) a Docker bridge container's TLS
    # handshake to RDS black-holes across the 9001->1500 MTU boundary; host
    # networking avoids it. The API binds 127.0.0.1:8081 on the host under host
    # networking (no -p needed).
    # /data is the durable EBS mount prepared above (#501): bind the HOST path,
    # never a Docker named volume (which would live on the ephemeral root disk).
    docker run -d --name signals --restart=always --network host \
      --env-file /etc/signals/signals.env \
      -v /etc/signals:/etc/signals:ro \
      -v /data:/data \
      ${var.image_uri} signals --config /etc/signals/signals.yaml
  EOT
}

# --- Durable data volume (#501) ---------------------------------------------
# A separate, encrypted EBS volume holds /data (signals.db). Its lifecycle is
# independent of the instance: user_data_replace_on_change replaces ONLY the
# instance (aws_instance.collector), while this volume is retained
# (prevent_destroy) and re-attached to the replacement — so collection history,
# instance identity, and cadence watermarks survive an AMI/instance upgrade
# (INV-DUR-01 / INV-DUR-02 / INV-DUR-05, R-DUR-05 / R-DUR-06).
resource "aws_ebs_volume" "data" {
  availability_zone = aws_instance.collector.availability_zone
  size              = var.data_volume_size
  type              = "gp3"
  encrypted         = true
  tags              = merge(local.tags, { Name = "${var.name_prefix}-data" })

  lifecycle {
    # Replacing the compute instance must never destroy the store. Destroying
    # the volume is a deliberate, separate operator action.
    prevent_destroy = true
  }
}

resource "aws_volume_attachment" "data" {
  device_name = var.data_device_name
  volume_id   = aws_ebs_volume.data.id
  instance_id = aws_instance.collector.id
  # Keep the volume when the instance is replaced; the replacement re-attaches
  # the same volume. Do NOT force-detach on destroy (that could corrupt the FS).
  stop_instance_before_detaching = true
}

# --- IAM identity: the passwordless enabler ---------------------------------

data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "collector" {
  name               = "${var.name_prefix}-collector"
  assume_role_policy = data.aws_iam_policy_document.assume.json
  tags               = local.tags
}

data "aws_iam_policy_document" "rds_connect" {
  statement {
    sid       = "RDSIAMConnect"
    actions   = ["rds-db:connect"]
    resources = [local.rds_connect_arn]
  }
}

resource "aws_iam_role_policy" "rds_connect" {
  name   = "${var.name_prefix}-rds-connect"
  role   = aws_iam_role.collector.id
  policy = data.aws_iam_policy_document.rds_connect.json
}

# Session Manager / Run Command access for the operator-gated verify steps —
# the templates provision no SSH key pair.
resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.collector.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "collector" {
  name = "${var.name_prefix}-collector"
  role = aws_iam_role.collector.name
  tags = local.tags
}

# --- Collector compute ------------------------------------------------------

resource "aws_instance" "collector" {
  ami                         = data.aws_ssm_parameter.al2023.value
  instance_type               = var.instance_type
  subnet_id                   = var.subnet_id
  vpc_security_group_ids      = var.security_group_ids
  iam_instance_profile        = aws_iam_instance_profile.collector.name
  user_data                   = local.user_data
  user_data_replace_on_change = true

  metadata_options {
    http_tokens   = "required" # IMDSv2 only
    http_endpoint = "enabled"
  }

  root_block_device {
    encrypted   = true
    volume_size = 20
  }

  tags = merge(local.tags, { Name = "${var.name_prefix}-collector" })
}
