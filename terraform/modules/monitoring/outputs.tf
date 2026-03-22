output "notification_channel_id" {
  value       = google_monitoring_notification_channel.email.id
  description = "Email notification channel ID"
}

output "alert_policy_ids" {
  value       = var.enable_alerts ? [for p in [google_monitoring_alert_policy.latency[0], google_monitoring_alert_policy.error_rate[0], google_monitoring_alert_policy.cpu_utilization[0]] : p.id] : []
  description = "Alert policy IDs"
}
