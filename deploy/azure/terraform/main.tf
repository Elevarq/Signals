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
    docker run -d --name signals --restart=always \
      -e AZURE_CLIENT_ID=${azurerm_user_assigned_identity.collector.client_id} \
      -e SIGNALS_API_TOKEN="$SIGNALS_API_TOKEN" \
      -v /etc/signals:/etc/signals:ro \
      -v signals-data:/data \
      -p 127.0.0.1:8081:8081 \
      ${var.image_uri} signals --config /etc/signals/signals.yaml
  EOT
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
