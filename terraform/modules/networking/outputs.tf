output "vpc_connector_id" {
  value       = google_vpc_access_connector.connector.id
  description = "VPC Access connector ID for Cloud Run"
}

output "vpc_id" {
  value       = google_compute_network.vpc.id
  description = "VPC network ID"
}
