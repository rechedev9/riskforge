output "bucket_name" {
  value       = google_storage_bucket.documents.name
  description = "Document storage bucket name"
}

output "bucket_url" {
  value       = google_storage_bucket.documents.url
  description = "Document storage bucket URL"
}

output "registry_url" {
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.docker.repository_id}"
  description = "Artifact Registry Docker repository URL"
}
