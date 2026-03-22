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

variable "connector_machine_type" {
  type        = string
  default     = "e2-micro"
  description = "Machine type for the VPC Access Connector. Use e2-standard-4 for prod (200 Mbps vs 2 Gbps throughput)."
}
