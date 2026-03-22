variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "region" {
  type        = string
  description = "GCP region for the key ring"
}

variable "environment" {
  type        = string
  description = "Environment name (dev, prod)"

  validation {
    condition     = contains(["dev", "prod"], var.environment)
    error_message = "environment must be dev or prod"
  }
}

variable "enable_cmek" {
  type        = bool
  default     = false
  description = "Enable Customer-Managed Encryption Keys. When false, no KMS resources are created."
}
