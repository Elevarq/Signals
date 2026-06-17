output "collector_identity_name" {
  description = "Managed identity display name — create the PG principal with this exact name (pgaadauth_create_principal)."
  value       = azurerm_user_assigned_identity.collector.name
}

output "collector_identity_client_id" {
  description = "Managed identity client ID — the azure_client_id the collector uses to select this identity."
  value       = azurerm_user_assigned_identity.collector.client_id
}

output "collector_identity_principal_id" {
  description = "Managed identity principal (object) ID."
  value       = azurerm_user_assigned_identity.collector.principal_id
}

output "vm_id" {
  description = "Collector VM resource ID."
  value       = azurerm_linux_virtual_machine.collector.id
}
