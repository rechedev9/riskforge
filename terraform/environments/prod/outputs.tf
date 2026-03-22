output "api_url" {
  description = "Cloud Run API service URL"
  value       = module.cloud_run_api.service_url
}

output "worker_url" {
  description = "Cloud Run worker service URL"
  value       = module.cloud_run_worker.service_url
}

output "spanner_instance" {
  description = "Spanner instance name"
  value       = module.spanner.instance_name
}

output "spanner_database" {
  description = "Spanner database name"
  value       = module.spanner.database_name
}

output "pubsub_topic" {
  description = "Pub/Sub topic name"
  value       = module.pubsub.topic_name
}

output "bucket_name" {
  description = "Document storage bucket name"
  value       = module.storage.bucket_name
}

output "registry_url" {
  description = "Artifact Registry URL"
  value       = module.storage.registry_url
}
