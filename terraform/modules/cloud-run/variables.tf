variable "service_name" {
  type        = string
  description = "Cloud Run service name"
}

variable "region" {
  type        = string
  description = "GCP region"
}

variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "image" {
  type        = string
  description = "Container image URI"
}

variable "min_instances" {
  type        = number
  default     = 0
  description = "Minimum instance count (0 for scale-to-zero)"
}

variable "max_instances" {
  type        = number
  default     = 10
  description = "Maximum instance count"
}

variable "env_vars" {
  type        = map(string)
  default     = {}
  description = "Plain-text environment variables (name => value)"
}

variable "secret_env_vars" {
  type = map(object({
    secret  = string
    version = string
  }))
  default     = {}
  description = "Secret Manager-backed environment variables (name => {secret, version})"
}

variable "resource_limits" {
  type = map(string)
  default = {
    cpu    = "1"
    memory = "512Mi"
  }
  description = "Container resource limits"
}

variable "container_port" {
  type        = number
  default     = 8080
  description = "Container port"
}

variable "service_account_email" {
  type        = string
  description = "Runtime service account email"
}

variable "ingress" {
  type        = string
  default     = "INGRESS_TRAFFIC_ALL"
  description = "Ingress traffic setting"
}

variable "vpc_connector_id" {
  type        = string
  default     = null
  description = "VPC Access connector ID"
}

variable "vpc_egress" {
  type        = string
  default     = "PRIVATE_RANGES_ONLY"
  description = "VPC egress setting"
}

variable "deletion_protection" {
  type        = bool
  default     = true
  description = "Enable deletion protection"
}

variable "allow_unauthenticated" {
  type        = bool
  default     = false
  description = "Allow unauthenticated access (allUsers invoker)"
}

variable "invoker_sa_email" {
  type        = string
  default     = ""
  description = "SA email granted roles/run.invoker when allow_unauthenticated=false"
}

variable "labels" {
  type        = map(string)
  default     = {}
  description = "Labels to apply to the service"
}
