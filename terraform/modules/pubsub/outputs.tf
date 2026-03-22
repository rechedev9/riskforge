output "topic_id" {
  value       = google_pubsub_topic.appetite_events.id
  description = "Appetite events topic ID"
}

output "topic_name" {
  value       = google_pubsub_topic.appetite_events.name
  description = "Appetite events topic name"
}

output "subscription_id" {
  value       = google_pubsub_subscription.appetite_events_push.id
  description = "Push subscription ID"
}
