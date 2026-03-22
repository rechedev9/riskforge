output "api_sa_email" {
  value       = google_service_account.cloud_run_api.email
  description = "Cloud Run API service account email"
}

output "worker_sa_email" {
  value       = google_service_account.cloud_run_worker.email
  description = "Cloud Run worker service account email"
}

output "pubsub_invoker_sa_email" {
  value       = google_service_account.pubsub_invoker.email
  description = "Pub/Sub push invoker service account email"
}
