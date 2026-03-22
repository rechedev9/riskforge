data "google_project" "current" {
  project_id = var.project_id
}

locals {
  pubsub_service_agent = "serviceAccount:service-${data.google_project.current.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

resource "google_pubsub_topic" "appetite_events" {
  name    = "appetite-events-${var.environment}"
  project = var.project_id

  message_retention_duration = "86400s"

  labels = {
    environment = var.environment
  }
}

resource "google_pubsub_topic" "appetite_events_dlq" {
  name    = "appetite-events-dlq-${var.environment}"
  project = var.project_id

  labels = {
    environment = var.environment
  }
}

resource "google_pubsub_subscription" "appetite_events_push" {
  name    = "appetite-events-push-${var.environment}"
  topic   = google_pubsub_topic.appetite_events.id
  project = var.project_id

  ack_deadline_seconds       = 20
  message_retention_duration = "604800s"

  push_config {
    push_endpoint = var.push_endpoint

    oidc_token {
      service_account_email = var.invoker_sa_email
      audience              = var.push_endpoint
    }
  }

  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.appetite_events_dlq.id
    max_delivery_attempts = 10
  }

  retry_policy {
    minimum_backoff = "10s"
    maximum_backoff = "600s"
  }

  expiration_policy {
    ttl = ""
  }

  labels = {
    environment = var.environment
  }
}

resource "google_pubsub_subscription" "appetite_events_dlq_pull" {
  name    = "appetite-events-dlq-pull-${var.environment}"
  topic   = google_pubsub_topic.appetite_events_dlq.id
  project = var.project_id

  ack_deadline_seconds       = 20
  message_retention_duration = "604800s"

  expiration_policy {
    ttl = ""
  }

  labels = {
    environment = var.environment
  }
}

# API SA can publish to the main topic
resource "google_pubsub_topic_iam_member" "api_publisher" {
  topic  = google_pubsub_topic.appetite_events.name
  role   = "roles/pubsub.publisher"
  member = "serviceAccount:${var.api_sa_email}"
}

# Worker SA can subscribe to the push subscription
resource "google_pubsub_subscription_iam_member" "worker_subscriber" {
  subscription = google_pubsub_subscription.appetite_events_push.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:${var.worker_sa_email}"
}

# Pub/Sub service agent can publish to DLQ topic (for dead letter forwarding)
resource "google_pubsub_topic_iam_member" "dlq_publisher" {
  topic  = google_pubsub_topic.appetite_events_dlq.name
  role   = "roles/pubsub.publisher"
  member = local.pubsub_service_agent
}

# Pub/Sub service agent can subscribe to push subscription (for dead letter forwarding)
resource "google_pubsub_subscription_iam_member" "dlq_subscriber" {
  subscription = google_pubsub_subscription.appetite_events_push.name
  role         = "roles/pubsub.subscriber"
  member       = local.pubsub_service_agent
}
