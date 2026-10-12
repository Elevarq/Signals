// Azure passwordless onboarding for Elevarq Signals (#112).
//
// Provisions a user-assigned managed identity for the collector and runs the
// collector on a Linux VM pre-wired for auth_method: azure_entra over
// verify-full TLS. The credential is a short-lived Entra OAuth2 token minted
// from the managed identity at connect time — no password is stored anywhere
// (INV001/INV002).
//
// The DB-side mapping (pgaadauth_create_principal + GRANT pg_monitor) is a
// one-time SQL step run as an Entra administrator; the PG principal name MUST
// equal the managed identity display name. See ../README.md.
//
// Deploy at resource-group scope:
//   az deployment group create -g <rg> -f main.bicep -p main.bicepparam

targetScope = 'resourceGroup'

@description('Azure region for the collector resources. Defaults to the resource group location.')
param location string = resourceGroup().location

@description('Prefix for created resource names. The managed identity name (= prefix + "-collector") MUST equal the PG principal created with pgaadauth_create_principal.')
param namePrefix string = 'signals'

@description('Flexible Server endpoint hostname (e.g. myserver.postgres.database.azure.com).')
param dbHost string

@description('PostgreSQL port.')
param dbPort int = 5432

@description('Database name to connect to.')
param dbName string

@description('Resource ID of an existing subnet for the collector NIC (must route to the Flexible Server).')
param subnetId string

@description('Optional NSG resource ID to associate with the collector NIC (must allow egress to the DB on dbPort). Leave empty to rely on the subnet NSG.')
param networkSecurityGroupId string = ''

@description('VM size for the collector.')
param vmSize string = 'Standard_B1s'

@description('Admin user for the collector VM (SSH key auth only; password auth is disabled).')
param adminUsername string = 'azureuser'

@description('SSH public key for the collector VM admin user. A public key is not a secret (INV001/INV002).')
param adminSshPublicKey string

@description('Elevarq Signals container image (pinned tag).')
param imageUri string = 'ghcr.io/elevarq/signals:1.0.2'

@description('URL of the CA bundle for sslmode=verify-full. Default is the DigiCert Global Root G2 that Azure Database for PostgreSQL Flexible Server chains to.')
param dbCaCertUrl string = 'https://cacerts.digicert.com/DigiCertGlobalRootG2.crt.pem'

@description('Signals environment (prod enforces verify-full TLS).')
param env string = 'prod'

@description('Collection interval.')
param pollInterval string = '5m'

@description('Extra tags applied to created resources.')
param tags object = {}

@description('Size (GiB) of the separate managed data disk that holds /data (signals.db). Retained and re-attached across VM/image replacement so history/identity/watermarks survive an upgrade (#501).')
param dataDiskSizeGb int = 10

@description('LUN the data disk attaches at (#501, R-DUR-02). The boot-time mount resolves the device via /dev/disk/azure/scsi1/lun<LUN> and mounts it at /data.')
param dataDiskLun int = 0

// The managed identity display name == the PG principal name (azure_entra
// requires user == Entra principal display name).
var identityName = '${namePrefix}-collector'

var commonTags = union({ 'app.kubernetes.io/name': 'signals', 'managed-by': 'bicep' }, tags)

resource collectorIdentity 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: identityName
  location: location
  tags: commonTags
}

// cloud-init for the collector VM. Bicep multi-line strings do not interpolate,
// so this is a single interpolated string with \n line breaks.
// Durable /data disk (#501, R-DUR-01..R-DUR-04): /data (signals.db) is backed
// by the separate managed data disk (resource `dataDisk` below, attached at a
// fixed LUN, createOption Attach on the VM so it is preserved — never from
// image), NOT the OS disk and NOT a Docker named volume. The mount is
// format-if-empty so a re-attached populated disk keeps its signals.db
// untouched (R-DUR-03), and /data is chowned to the non-root container user
// uid 10001 / group 10001 (R-DUR-04). The docker run binds the HOST /data path,
// never a named volume on the ephemeral OS disk.
var cloudInit ='#!/bin/bash\nset -euo pipefail\napt-get update -y\napt-get install -y docker.io curl openssl\nsystemctl enable --now docker\nmkdir -p /etc/signals\nDATA_DEV="$(readlink -f /dev/disk/azure/scsi1/lun${dataDiskLun} 2>/dev/null || true)"\nif [ -z "$DATA_DEV" ] || [ ! -b "$DATA_DEV" ]; then for d in /dev/sd*; do [ -b "$d" ] || continue; case "$d" in *[0-9]) continue;; esac; if lsblk -no MOUNTPOINT "$d" | grep -qE "^(/|/boot)"; then continue; fi; DATA_DEV="$d"; done; fi\nmkdir -p /data\nif ! blkid "$DATA_DEV" >/dev/null 2>&1; then mkfs.ext4 -F "$DATA_DEV"; fi\nDATA_UUID="$(blkid -s UUID -o value "$DATA_DEV")"\ngrep -q "$DATA_UUID" /etc/fstab || echo "UUID=$DATA_UUID /data ext4 defaults,nofail 0 2" >> /etc/fstab\nmount /data || mount "$DATA_DEV" /data\nchown 10001:10001 /data\n# Azure Flexible Server CA bundle for sslmode=verify-full.\ncurl -fsSL ${dbCaCertUrl} -o /etc/signals/azure-ca.pem\ncat > /etc/signals/signals.yaml <<\'YAML\'\nenv: ${env}\nsignals:\n  poll_interval: ${pollInterval}\ndatabase:\n  path: /data/signals.db\n  wal: true\napi:\n  listen_addr: "127.0.0.1:8081"\ntargets:\n  - name: ${dbName}-azure\n    host: ${dbHost}\n    port: ${dbPort}\n    dbname: ${dbName}\n    user: ${identityName}\n    auth_method: azure_entra\n    azure_client_id: ${collectorIdentity.properties.clientId}\n    sslmode: verify-full\n    sslrootcert_file: /etc/signals/azure-ca.pem\nYAML\nchmod 0644 /etc/signals/signals.yaml /etc/signals/azure-ca.pem\nSIGNALS_API_TOKEN="$(openssl rand -hex 32)"\numask 077\necho "$SIGNALS_API_TOKEN" > /root/signals-api-token\ndocker run -d --name signals --restart=always -e AZURE_CLIENT_ID=${collectorIdentity.properties.clientId} -e SIGNALS_API_TOKEN="$SIGNALS_API_TOKEN" -v /etc/signals:/etc/signals:ro -v /data:/data -p 127.0.0.1:8081:8081 ${imageUri} signals --config /etc/signals/signals.yaml\n'

// Durable data disk (#501). A separate managed disk holds /data (signals.db).
// Managed disks are encrypted at rest with platform-managed keys by default
// (R-DUR-06). It is attached to the VM with createOption 'Attach' (never
// 'FromImage'/'Empty' on the VM), so a VM replacement that re-attaches this
// same disk preserves collection history, instance identity, and cadence
// watermarks (INV-DUR-01/02/05). Deleting the VM does NOT delete this disk
// (default managed-disk deleteOption is Detach), so the store is retained
// across compute replacement (R-DUR-05).
resource dataDisk 'Microsoft.Compute/disks@2024-03-02' = {
  name: '${namePrefix}-data'
  location: location
  tags: commonTags
  sku: {
    name: 'Standard_LRS'
  }
  properties: {
    creationData: {
      createOption: 'Empty'
    }
    diskSizeGB: dataDiskSizeGb
  }
}

resource nic 'Microsoft.Network/networkInterfaces@2023-09-01' = {
  name: '${namePrefix}-collector-nic'
  location: location
  tags: commonTags
  properties: {
    ipConfigurations: [
      {
        name: 'internal'
        properties: {
          subnet: {
            id: subnetId
          }
          privateIPAllocationMethod: 'Dynamic'
        }
      }
    ]
    networkSecurityGroup: empty(networkSecurityGroupId) ? null : {
      id: networkSecurityGroupId
    }
  }
}

resource vm 'Microsoft.Compute/virtualMachines@2024-03-01' = {
  name: '${namePrefix}-collector'
  location: location
  tags: commonTags
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${collectorIdentity.id}': {}
    }
  }
  properties: {
    hardwareProfile: {
      vmSize: vmSize
    }
    osProfile: {
      computerName: '${namePrefix}-collector'
      adminUsername: adminUsername
      customData: base64(cloudInit)
      linuxConfiguration: {
        disablePasswordAuthentication: true
        ssh: {
          publicKeys: [
            {
              path: '/home/${adminUsername}/.ssh/authorized_keys'
              keyData: adminSshPublicKey
            }
          ]
        }
      }
    }
    storageProfile: {
      imageReference: {
        publisher: 'Canonical'
        offer: 'ubuntu-24_04-lts'
        sku: 'server'
        version: 'latest'
      }
      osDisk: {
        createOption: 'FromImage'
        caching: 'ReadWrite'
        managedDisk: {
          storageAccountType: 'Standard_LRS'
        }
      }
      // #501: attach the durable data disk at a fixed LUN. createOption 'Attach'
      // preserves the existing disk (never reformats from image), and
      // deleteOption 'Detach' keeps the disk when the VM is deleted/replaced, so
      // /data (signals.db) survives a VM upgrade (R-DUR-02/R-DUR-05).
      dataDisks: [
        {
          lun: dataDiskLun
          createOption: 'Attach'
          caching: 'ReadWrite'
          deleteOption: 'Detach'
          managedDisk: {
            id: dataDisk.id
          }
        }
      ]
    }
    networkProfile: {
      networkInterfaces: [
        {
          id: nic.id
        }
      ]
    }
  }
}

@description('Managed identity display name — create the PG principal with this exact name (pgaadauth_create_principal).')
output collectorIdentityName string = collectorIdentity.name

@description('Managed identity client ID — the azure_client_id the collector uses to select this identity.')
output collectorIdentityClientId string = collectorIdentity.properties.clientId

@description('Managed identity principal (object) ID.')
output collectorIdentityPrincipalId string = collectorIdentity.properties.principalId

@description('Collector VM resource ID.')
output vmId string = vm.id
