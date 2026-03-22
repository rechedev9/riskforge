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

variable "spanner_processing_units" {
  type        = number
  description = "Spanner instance processing units (100 PU = 1 node)"
}

variable "deletion_protection" {
  type        = bool
  description = "Enable deletion protection on Spanner database"
}

variable "sa_emails" {
  type        = list(string)
  description = "Service account emails to grant roles/spanner.databaseUser"
}
