# Azure passwordless onboarding for Elevarq Signals (#112).
#
# Provisions a user-assigned managed identity for the collector and runs the
# collector on a Linux VM pre-wired for auth_method: azure_entra over
# verify-full TLS. The credential is a short-lived Entra OAuth2 token minted
# from the managed identity at connect time — no password is stored anywhere
# (INV001/INV002).
#
# The DB-side mapping (pgaadauth_create_principal + GRANT pg_monitor) is a
# one-time SQL step run as an Entra administrator; the PG principal name MUST
# equal the managed identity display name. See ../README.md.

data "azurerm_resource_group" "rg" {
  name = var.resource_group_name
}

locals {
  tags = merge({ "app.kubernetes.io/name" = "signals", "managed-by" = "terraform" }, var.tags)

  # The managed identity display name == the PG principal name (azure_entra
  # requires user == Entra principal display name).
  identity_name = "${var.name_prefix}-collector"

  user_data = <<-EOT
    #!/bin/bash
    set -euo pipefail
    apt-get update -y
    apt-get install -y docker.io curl openssl
    systemctl enable --now docker
    mkdir -p /etc/signals
    # --- Durable /data disk (#501, R-DUR-01..R-DUR-04) ------------------------
    # /data (signals.db: identity, history, cadence watermarks) is backed by the
    # separate managed data disk (azurerm_managed_disk below: prevent_destroy;
    # managed disks are platform-encrypted at rest), attached at a fixed LUN —
    # NOT the OS disk and NOT a Docker named volume. Azure exposes the LUN at a
    # stable path under /dev/disk/azure/scsi1/. Mount format-if-empty so a
    # re-attached populated disk keeps its signals.db untouched (R-DUR-03).
    DATA_DEV="$(readlink -f /dev/disk/azure/scsi1/lun${var.data_disk_lun} 2>/dev/null || true)"
    if [ -z "$DATA_DEV" ] || [ ! -b "$DATA_DEV" ]; then
      for d in /dev/sd*; do
        [ -b "$d" ] || continue
        case "$d" in *[0-9]) continue;; esac
        if lsblk -no MOUNTPOINT "$d" | grep -qE '^(/|/boot)'; then continue; fi
        DATA_DEV="$d"
      done
    fi
    mkdir -p /data
    if ! blkid "$DATA_DEV" >/dev/null 2>&1; then
      mkfs.ext4 -F "$DATA_DEV"
    fi
    DATA_UUID="$(blkid -s UUID -o value "$DATA_DEV")"
    grep -q "$DATA_UUID" /etc/fstab || \
      echo "UUID=$DATA_UUID /data ext4 defaults,nofail 0 2" >> /etc/fstab
    mount /data || mount "$DATA_DEV" /data
    # Non-root container user (uid 10001, group 10001) owns /data (R-DUR-04).
    chown 10001:10001 /data
    # Azure Flexible Server CA bundle for sslmode=verify-full.
    curl -fsSL ${var.db_ca_cert_url} -o /etc/signals/azure-ca.pem
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
      - name: ${var.db_name}-azure
        host: ${var.db_host}
        port: ${var.db_port}
        dbname: ${var.db_name}
        user: ${local.identity_name}
        auth_method: azure_entra
        azure_client_id: ${azurerm_user_assigned_identity.collector.client_id}
        sslmode: verify-full
        sslrootcert_file: /etc/signals/azure-ca.pem
    YAML
    # The config carries no secret; make the bind-mounted files world-readable
    # so the non-root container (uid 10001) can read them.
    chmod 0644 /etc/signals/signals.yaml /etc/signals/azure-ca.pem
    # Mint a strong control-plane API token, passed to the container via the
    # environment (alongside AZURE_CLIENT_ID) and stored root-only, outside the
    # bind mount, so the operator can authenticate the verify step.
    SIGNALS_API_TOKEN="$(openssl rand -hex 32)"
    umask 077
    printf '%s\n' "$SIGNALS_API_TOKEN" > /root/signals-api-token
    # ENTRYPOINT is `tini --` with CMD `signals`; the `signals` arg below is
    # required, else the image args replace CMD and tini execs --config.
    # /data is the durable managed-disk mount prepared above (#501): bind the
    # HOST path, never a Docker named volume (which lives on the ephemeral OS disk).
    docker run -d --name signals --restart=always \
      -e AZURE_CLIENT_ID=${azurerm_user_assigned_identity.collector.client_id} \
      -e SIGNALS_API_TOKEN="$SIGNALS_API_TOKEN" \
      -v /etc/signals:/etc/signals:ro \
      -v /data:/data \
      -p 127.0.0.1:8081:8081 \
      ${var.image_uri} signals --config /etc/signals/signals.yaml
  EOT
}

# --- Durable data disk (#501) -----------------------------------------------
# A separate managed data disk holds /data (signals.db). Its lifecycle is
# independent of the VM: prevent_destroy retains it, and the replacement VM
# re-attaches it at the same LUN, so collection history, instance identity, and
# cadence watermarks survive a VM/image upgrade (INV-DUR-01/02/05, R-DUR-05).
# Managed disks are encrypted at rest with platform-managed keys by default
# (R-DUR-06).
resource "azurerm_managed_disk" "data" {
  name                 = "${var.name_prefix}-data"
  resource_group_name  = data.azurerm_resource_group.rg.name
  location             = var.location
  storage_account_type = "Standard_LRS"
  create_option        = "Empty"
  disk_size_gb         = var.data_disk_size_gb
  tags                 = local.tags

  lifecycle {
    # Replacing the VM must never destroy the store.
    prevent_destroy = true
  }
}

resource "azurerm_virtual_machine_data_disk_attachment" "data" {
  managed_disk_id    = azurerm_managed_disk.data.id
  virtual_machine_id = azurerm_linux_virtual_machine.collector.id
  lun                = var.data_disk_lun
  caching            = "ReadWrite"
}

# --- Managed identity: the passwordless enabler -----------------------------

resource "azurerm_user_assigned_identity" "collector" {
  name                = local.identity_name
  resource_group_name = data.azurerm_resource_group.rg.name
  location            = var.location
  tags                = local.tags
}

# --- Collector compute ------------------------------------------------------

resource "azurerm_network_interface" "collector" {
  name                = "${var.name_prefix}-collector-nic"
  resource_group_name = data.azurerm_resource_group.rg.name
  location            = var.location
  tags                = local.tags

  ip_configuration {
    name                          = "internal"
    subnet_id                     = var.subnet_id
    private_ip_address_allocation = "Dynamic"
  }
}

resource "azurerm_network_interface_security_group_association" "collector" {
  count                     = var.network_security_group_id == "" ? 0 : 1
  network_interface_id      = azurerm_network_interface.collector.id
  network_security_group_id = var.network_security_group_id
}

resource "azurerm_linux_virtual_machine" "collector" {
  name                = "${var.name_prefix}-collector"
  resource_group_name = data.azurerm_resource_group.rg.name
  location            = var.location
  size                = var.vm_size
  admin_username      = var.admin_username
  network_interface_ids = [
    azurerm_network_interface.collector.id,
  ]
  custom_data = base64encode(local.user_data)

  admin_ssh_key {
    username   = var.admin_username
    public_key = var.admin_ssh_public_key
  }

  # No password auth — SSH key only.
  disable_password_authentication = true

  identity {
    type         = "UserAssigned"
    identity_ids = [azurerm_user_assigned_identity.collector.id]
  }

  os_disk {
    caching              = "ReadWrite"
    storage_account_type = "Standard_LRS"
  }

  source_image_reference {
    publisher = "Canonical"
    offer     = "ubuntu-24_04-lts"
    sku       = "server"
    version   = "latest"
  }

  tags = local.tags
}
