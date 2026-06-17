output "collector_role_arn" {
  description = "IAM role the collector runs under (attach elsewhere if not using the bundled EC2 instance)."
  value       = aws_iam_role.collector.arn
}

output "rds_connect_resource_arn" {
  description = "The rds-db:connect resource ARN scoped to db_user on this instance."
  value       = local.rds_connect_arn
}

output "instance_id" {
  description = "Collector EC2 instance id."
  value       = aws_instance.collector.id
}
