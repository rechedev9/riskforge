variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "environment" {
  type        = string
  description = "Environment name (dev, prod)"
}

variable "push_endpoint" {
  type        = string
  description = "Cloud Run worker URL for push subscription"
}

variable "invoker_sa_email" {
  type        = string
  description = "Pub/Sub invoker SA email for OIDC token"
}

variable "worker_sa_email" {
  type        = string
  description = "Worker SA email for subscriber role"
}

variable "api_sa_email" {
  type        = string
  description = "API SA email for publisher role"
}
