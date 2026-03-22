output "service_url" {
  value       = google_cloud_run_v2_service.service.uri
  description = "Cloud Run service HTTPS URL"
}

output "service_name" {
  value       = google_cloud_run_v2_service.service.name
  description = "Cloud Run service name"
}

output "service_id" {
  value       = google_cloud_run_v2_service.service.id
  description = "Cloud Run service ID"
}
