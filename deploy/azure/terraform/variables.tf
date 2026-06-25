# Inputs for the Azure passwordless (Entra ID) onboarding module (#112).
# No secrets here — azure_entra mints a short-lived Entra OAuth2 token from
# the collector VM's user-assigned managed identity (INV001/INV002). The SSH
# public key is a public credential, not a secret.

variable "resource_group_name" {
  type        = string
  description = "Existing resource group to create the collector resources in."
}

variable "location" {
  type        = string
  description = "Azure region for the collector resources (e.g. westeurope)."
}

variable "name_prefix" {
  type        = string
  description = "Prefix for created resource names. The managed identity's name (= this prefix + '-collector') MUST equal the PG principal created with pgaadauth_create_principal."
  default     = "signals"
}

variable "db_host" {
  type        = string
  description = "Flexible Server endpoint hostname (e.g. myserver.postgres.database.azure.com)."
}

variable "db_port" {
  type        = number
  description = "PostgreSQL port."
  default     = 5432
}

variable "db_name" {
  type        = string
  description = "Database name to connect to."
}

variable "subnet_id" {
  type        = string
  description = "Resource ID of an existing subnet for the collector NIC (must route to the Flexible Server)."
}

variable "network_security_group_id" {
  type        = string
  description = "Optional NSG to associate with the collector NIC (must allow egress to the DB on db_port). Leave empty to rely on the subnet's NSG."
  default     = ""
}

variable "vm_size" {
  type        = string
  description = "VM size for the collector."
  default     = "Standard_B1s"
}

variable "admin_username" {
  type        = string
  description = "Admin user for the collector VM (SSH key auth only; password auth is disabled)."
  default     = "azureuser"
}

variable "admin_ssh_public_key" {
  type        = string
  description = "SSH public key for the collector VM admin user. A public key is not a secret (INV001/INV002)."
}

variable "image_uri" {
  type        = string
  description = "Elevarq Signals container image (pinned tag)."
  default     = "ghcr.io/elevarq/signals:0.10.0-beta.7"
}

variable "db_ca_cert_url" {
  type        = string
  description = "URL of the CA bundle for sslmode=verify-full. Default is the DigiCert Global Root G2 that Azure Database for PostgreSQL Flexible Server chains to."
  default     = "https://cacerts.digicert.com/DigiCertGlobalRootG2.crt.pem"
}

variable "env" {
  type        = string
  description = "Signals environment (prod enforces verify-full TLS)."
  default     = "prod"
}

variable "poll_interval" {
  type        = string
  description = "Collection interval."
  default     = "5m"
}

variable "tags" {
  type        = map(string)
  description = "Extra tags applied to created resources."
  default     = {}
}
