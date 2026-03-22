variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "service_name" {
  type        = string
  description = "Cloud Run service name for alert filters"
}

variable "service_url" {
  type        = string
  description = "Cloud Run service URL for uptime check"
}

variable "notification_email" {
  type        = string
  description = "Email address for alert notifications"
}

variable "enable_alerts" {
  type        = bool
  default     = true
  description = "Enable alert policies and uptime checks (disable for dev to reduce noise)"
}
