variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "region" {
  type        = string
  description = "GCP region"
}

variable "environment" {
  type        = string
  description = "Environment name (dev, prod)"
}

variable "api_sa_email" {
  type        = string
  description = "API SA email for bucket IAM"
}

variable "worker_sa_email" {
  type        = string
  description = "Worker SA email for bucket IAM"
}
