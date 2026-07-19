# Inputs for the GCP passwordless (Cloud SQL IAM) onboarding module (#113).
# No secrets here — gcp_cloudsql_iam mints a short-lived Google OAuth2 token
# from the collector VM's attached service account (INV001/INV002). The
# server CA certificate is public, not a secret.

variable "project_id" {
  type        = string
  description = "GCP project ID for the collector resources."
}

variable "region" {
  type        = string
  description = "GCP region (e.g. europe-west1)."
}

variable "zone" {
  type        = string
  description = "GCP zone for the collector VM (e.g. europe-west1-b)."
}

variable "name_prefix" {
  type        = string
  description = "Prefix for created resource names."
  default     = "signals"
}

variable "db_host" {
  type        = string
  description = "Cloud SQL endpoint — private IP, or the Cloud SQL proxy endpoint."
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

variable "instance_name" {
  type        = string
  description = "Cloud SQL instance name. When set, the module creates the IAM DB user declaratively; leave empty to create it out-of-band (see README)."
  default     = ""
}

variable "db_server_ca_cert" {
  type        = string
  description = "The Cloud SQL instance server-ca.pem contents (PEM), used for sslmode=verify-full. This is a public certificate, not a secret. Get it from the Cloud SQL console or: gcloud sql instances describe <instance> --format='value(serverCaCert.cert)'."
}

variable "network" {
  type        = string
  description = "VPC network self_link or name for the collector VM (must route to the Cloud SQL private IP)."
}

variable "subnetwork" {
  type        = string
  description = "Subnetwork self_link or name for the collector VM."
}

variable "gcp_impersonate_service_account" {
  type        = string
  description = "Optional service account email for the collector to impersonate (the VM SA must hold roles/iam.serviceAccountTokenCreator on it). Empty disables impersonation."
  default     = ""
}

variable "machine_type" {
  type        = string
  description = "GCE machine type for the collector."
  default     = "e2-small"
}

variable "image" {
  type        = string
  description = "Boot image for the collector VM."
  default     = "projects/debian-cloud/global/images/family/debian-12"
}

variable "image_uri" {
  type        = string
  description = "Elevarq Signals container image (pinned tag)."
  default     = "ghcr.io/elevarq/signals:1.0.2"
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

variable "labels" {
  type        = map(string)
  description = "Extra labels applied to created resources."
  default     = {}
}
