output "notification_channel_id" {
  value       = google_monitoring_notification_channel.email.id
  description = "Email notification channel ID"
}

output "alert_policy_ids" {
  value = [
    google_monitoring_alert_policy.latency.id,
    google_monitoring_alert_policy.error_rate.id,
    google_monitoring_alert_policy.cpu_utilization.id,
  ]
  description = "Alert policy IDs"
}
