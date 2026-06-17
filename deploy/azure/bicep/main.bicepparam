// Example parameters for the Azure passwordless onboarding (#112).
// No secrets here — adminSshPublicKey is a public key, not a secret.
// Copy and edit, then: az deployment group create -g <rg> -f main.bicep -p main.bicepparam
using './main.bicep'

param dbHost = 'myserver.postgres.database.azure.com'
param dbName = 'appdb'
param subnetId = '/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/my-rg/providers/Microsoft.Network/virtualNetworks/my-vnet/subnets/collector'
param adminSshPublicKey = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... you@example.com'
