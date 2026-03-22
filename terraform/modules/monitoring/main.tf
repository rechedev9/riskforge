resource "google_monitoring_alert_policy" "latency" {
  count = var.enable_alerts ? 1 : 0

  display_name = "Cloud Run ${var.service_name} - High Latency"
  project      = var.project_id
  combiner     = "OR"

  conditions {
    display_name = "P99 latency > 2s"

    condition_threshold {
      filter = <<-EOT
        resource.type = "cloud_run_revision"
        AND resource.labels.service_name = "${var.service_name}"
        AND metric.type = "run.googleapis.com/request_latencies"
      EOT

      comparison      = "COMPARISON_GT"
      threshold_value = 2000
      duration        = "300s"

      aggregations {
        alignment_period     = "60s"
        per_series_aligner   = "ALIGN_PERCENTILE_99"
        cross_series_reducer = "REDUCE_MAX"
      }

      trigger {
        count = 1
      }
    }
  }

  notification_channels = [
    google_monitoring_notification_channel.email.name,
  ]

  documentation {
    content   = "P99 request latency for Cloud Run service `$${resource.labels.service_name}` exceeded 2 seconds for over 5 minutes."
    mime_type = "text/markdown"
  }

  user_labels = {
    severity = "warning"
    service  = var.service_name
  }
}

resource "google_monitoring_alert_policy" "error_rate" {
  count = var.enable_alerts ? 1 : 0

  display_name = "Cloud Run ${var.service_name} - High Error Rate"
  project      = var.project_id
  combiner     = "OR"

  conditions {
    display_name = "5xx error count > 5 per minute"

    condition_threshold {
      filter = <<-EOT
        resource.type = "cloud_run_revision"
        AND resource.labels.service_name = "${var.service_name}"
        AND metric.type = "run.googleapis.com/request_count"
        AND metric.labels.response_code_class = "5xx"
      EOT

      comparison      = "COMPARISON_GT"
      threshold_value = 5
      duration        = "300s"

      aggregations {
        alignment_period     = "60s"
        per_series_aligner   = "ALIGN_RATE"
        cross_series_reducer = "REDUCE_SUM"
      }

      trigger {
        count = 1
      }
    }
  }

  notification_channels = [
    google_monitoring_notification_channel.email.name,
  ]

  documentation {
    content   = "Error rate for Cloud Run service `$${resource.labels.service_name}` exceeded threshold. Check Cloud Run logs for details."
    mime_type = "text/markdown"
  }

  user_labels = {
    severity = "critical"
    service  = var.service_name
  }
}

resource "google_monitoring_alert_policy" "cpu_utilization" {
  count = var.enable_alerts ? 1 : 0

  display_name = "Cloud Run ${var.service_name} - CPU Utilization > 80%"
  project      = var.project_id
  combiner     = "OR"

  conditions {
    display_name = "CPU > 80% for 5m"

    condition_threshold {
      filter = <<-EOT
        resource.type = "cloud_run_revision"
        AND resource.labels.service_name = "${var.service_name}"
        AND metric.type = "run.googleapis.com/container/cpu/utilizations"
      EOT

      comparison      = "COMPARISON_GT"
      threshold_value = 0.8
      duration        = "300s"

      aggregations {
        alignment_period     = "60s"
        per_series_aligner   = "ALIGN_PERCENTILE_99"
        cross_series_reducer = "REDUCE_MAX"
      }

      trigger {
        count = 1
      }
    }
  }

  notification_channels = [
    google_monitoring_notification_channel.email.name,
  ]

  documentation {
    content   = "CPU utilization for Cloud Run service `$${resource.labels.service_name}` exceeded 80% for over 5 minutes."
    mime_type = "text/markdown"
  }

  user_labels = {
    severity = "warning"
    service  = var.service_name
  }
}

resource "google_monitoring_uptime_check_config" "api_health" {
  count = var.enable_alerts ? 1 : 0

  display_name = "${var.service_name} Health Check"
  project      = var.project_id
  timeout      = "10s"
  period       = "60s"

  http_check {
    path           = "/health"
    port           = 443
    use_ssl        = true
    validate_ssl   = true
    request_method = "GET"

    accepted_response_status_codes {
      status_class = "STATUS_CLASS_2XX"
    }
  }

  monitored_resource {
    type = "uptime_url"
    labels = {
      project_id = var.project_id
      host       = trimprefix(var.service_url, "https://")
    }
  }
}
