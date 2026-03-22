output "state_bucket_name" {
  value       = google_storage_bucket.terraform_state.name
  description = "GCS bucket for Terraform remote state"
}

output "wif_provider_name" {
  value       = google_iam_workload_identity_pool_provider.github.name
  description = "Full resource name of the WIF provider"
}

output "terraform_sa_email" {
  value       = google_service_account.terraform.email
  description = "Terraform automation service account email"
}
