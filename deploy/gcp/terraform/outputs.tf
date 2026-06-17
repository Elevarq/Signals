output "collector_service_account_email" {
  description = "Service account the collector runs under (attach elsewhere / grant Cloud SQL access)."
  value       = google_service_account.collector.email
}

output "collector_db_user" {
  description = "IAM DB user / PG role name — GRANT pg_monitor to this (SA email without .gserviceaccount.com)."
  value       = local.pg_user
}

output "instance_id" {
  description = "Collector GCE instance id."
  value       = google_compute_instance.collector.instance_id
}
