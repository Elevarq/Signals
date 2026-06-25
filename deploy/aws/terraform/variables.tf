# Inputs for the AWS passwordless (RDS IAM) onboarding module (#111).
# No secrets here — aws_rds_iam mints a short-lived token from the EC2
# instance's IAM identity (INV001/INV002).

variable "region" {
  type        = string
  description = "AWS region of the RDS/Aurora instance and where the collector runs."
}

variable "name_prefix" {
  type        = string
  description = "Prefix for created resource names."
  default     = "signals"
}

variable "db_host" {
  type        = string
  description = "RDS/Aurora endpoint hostname (e.g. mydb.abc123.us-east-1.rds.amazonaws.com)."
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

variable "db_user" {
  type        = string
  description = "PostgreSQL role granted rds_iam (least-privilege, pg_monitor)."
  default     = "signals"
}

variable "db_resource_id" {
  type        = string
  description = <<-EOT
    The RDS instance/cluster DbiResourceId (e.g. db-ABCDEFGH...), NOT the DB
    identifier. Used to scope the rds-db:connect IAM policy. Find it with:
    aws rds describe-db-instances --db-instance-identifier <id> \
      --query 'DBInstances[0].DbiResourceId' --output text
  EOT
}

variable "subnet_id" {
  type        = string
  description = "Subnet for the collector EC2 instance (must route to the RDS instance)."
}

variable "security_group_ids" {
  type        = list(string)
  description = "Security group(s) for the collector — must allow egress to the DB on db_port."
}

variable "instance_type" {
  type        = string
  description = "EC2 instance type for the collector."
  default     = "t3.small"
}

variable "image_uri" {
  type        = string
  description = "Elevarq Signals container image (pinned tag)."
  default     = "ghcr.io/elevarq/signals:0.10.0-beta.7"
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
