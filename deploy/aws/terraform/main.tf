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
    docker run -d --name signals --restart=always \
      --env-file /etc/signals/signals.env \
      -v /etc/signals:/etc/signals:ro \
      -v signals-data:/data \
      -p 127.0.0.1:8081:8081 \
      ${var.image_uri} signals --config /etc/signals/signals.yaml
  EOT
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
