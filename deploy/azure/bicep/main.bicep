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
param imageUri string = 'ghcr.io/elevarq/signals:0.10.0-beta.5'

@description('URL of the CA bundle for sslmode=verify-full. Default is the DigiCert Global Root G2 that Azure Database for PostgreSQL Flexible Server chains to.')
param dbCaCertUrl string = 'https://cacerts.digicert.com/DigiCertGlobalRootG2.crt.pem'

@description('Signals environment (prod enforces verify-full TLS).')
param arqEnv string = 'prod'

@description('Collection interval.')
param pollInterval string = '5m'

@description('Extra tags applied to created resources.')
param tags object = {}

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
var cloudInit ='#!/bin/bash\nset -euo pipefail\napt-get update -y\napt-get install -y docker.io curl\nsystemctl enable --now docker\nmkdir -p /etc/arq\n# Azure Flexible Server CA bundle for sslmode=verify-full.\ncurl -fsSL ${dbCaCertUrl} -o /etc/arq/azure-ca.pem\ncat > /etc/arq/signals.yaml <<\'YAML\'\nenv: ${arqEnv}\nsignals:\n  poll_interval: ${pollInterval}\ndatabase:\n  path: /data/arq-signals.db\n  wal: true\napi:\n  listen_addr: "127.0.0.1:8081"\ntargets:\n  - name: ${dbName}-azure\n    host: ${dbHost}\n    port: ${dbPort}\n    dbname: ${dbName}\n    user: ${identityName}\n    auth_method: azure_entra\n    azure_client_id: ${collectorIdentity.properties.clientId}\n    sslmode: verify-full\n    sslrootcert_file: /etc/arq/azure-ca.pem\nYAML\ndocker run -d --name signals --restart=always -e AZURE_CLIENT_ID=${collectorIdentity.properties.clientId} -v /etc/arq:/etc/arq:ro -v arq-data:/data -p 127.0.0.1:8081:8081 ${imageUri} --config /etc/arq/signals.yaml\n'

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
