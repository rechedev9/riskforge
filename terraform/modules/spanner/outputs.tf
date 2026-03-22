output "instance_name" {
  value       = google_spanner_instance.main.name
  description = "Spanner instance name"
}

output "database_name" {
  value       = google_spanner_database.main.name
  description = "Spanner database name"
}
