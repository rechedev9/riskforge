# Technical Design: Terraform GCP Module

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Technical Approach

This change creates a complete Terraform infrastructure-as-code layer for the appetite-engine application under `terraform/` (matching the CLAUDE.md architecture section and existing `.gitignore` entries). The infrastructure is decomposed into 7 reusable modules (`iam`, `networking`, `spanner`, `cloud-run`, `pubsub`, `storage`, `monitoring`), a bootstrap directory for chicken-and-egg resources, and 2 environment configurations (`dev`, `prod`).

All HCL follows patterns from `docs/terraform-gcp.md` exactly: `hashicorp/google ~> 6.0` provider, Cloud Run v2 API, Spanner with `processing_units`, Pub/Sub push with OIDC, VPC Access connector on `/28` subnet, and `google_monitoring_alert_policy` for observability. Every resource uses raw `google_*` provider resources -- no community modules -- to demonstrate deep GCP knowledge as a portfolio piece.

The environment layer wires modules together via outputs-to-inputs: `iam` produces SA emails consumed by 4 other modules; `networking` produces the VPC connector ID consumed by `cloud-run`; `cloud-run` produces service URLs consumed by `pubsub` and `monitoring`. The `cloud-run` module is generic and instantiated twice per environment (API + worker) with different ingress, scaling, and env var configurations. A GitHub Actions CI/CD workflow uses Workload Identity Federation for keyless auth, plans on PR, and applies on merge.

## Architecture Decisions

| # | Decision | Choice | Alternatives Considered | Rationale |
|---|----------|--------|-------------------------|-----------|
| 1 | Root directory naming | `terraform/` | `infrastructure/` (reference doc default) | CLAUDE.md architecture section and `.gitignore` already use `terraform/` prefix; avoids churn and inconsistency |
| 2 | Resource style | Raw `google_*` resources only | Community modules (e.g., `terraform-google-modules/*`), Terragrunt wrappers | Portfolio piece demonstrating deep GCP knowledge; `docs/terraform-gcp.md` provides all patterns; no external dependency risk |
| 3 | Cloud Run module reuse | Single generic module instantiated 2x per env | Separate `cloud-run-api` and `cloud-run-worker` modules | API and worker share 90% of config (image, scaling, SA, VPC); dynamic blocks and variables handle ingress/env var differences; DRY without premature abstraction |
| 4 | IAM binding style | `google_project_iam_member` (additive) | `google_project_iam_binding` (authoritative per-role), `google_project_iam_policy` (full policy) | Additive bindings do not clobber bindings managed outside Terraform; matches reference doc best practices |
| 5 | Spanner capacity unit | `processing_units` (100 PU minimum) | `num_nodes` (1 node minimum = 1000 PU) | Fine-grained control; 100 PU is the cheapest option for dev (~$65/mo); matches reference doc pattern |
| 6 | VPC connector subnet | Explicit `google_compute_subnetwork` + `subnet` block | Inline `ip_cidr_range` on connector directly | Explicit subnet gives more control, shows up in VPC console, follows the "VPC Connector with Existing Subnet" pattern in reference doc |
| 7 | Bootstrap state management | Local state (no remote backend) | Commit bootstrap state to repo, use a separate bucket | Bootstrap creates the state bucket itself -- cannot use it as its own backend; local state is the standard pattern for this chicken-and-egg scenario |
| 8 | CI/CD workflow structure | Matrix strategy with `[dev, prod]` | Separate workflows per environment, Terragrunt run-all | Matrix is simpler; single workflow file; GitHub environment protection rules gate prod apply |
| 9 | Secret Manager approach | Placeholder `google_secret_manager_secret` resources only | Full secret version management in Terraform, external secret management | Secret values must never appear in Terraform state or code; create empty shells, populate manually post-apply |
| 10 | Notification channel type | Email only | Email + Slack, Email + PagerDuty | Start simple; email is zero-config; Slack/PagerDuty can be added later as additional `google_monitoring_notification_channel` resources without breaking changes |

## Data Flow

### Terraform Apply -- Environment Provisioning

```
terraform apply (environments/dev/ or environments/prod/)
  |
  +-> module.iam
  |     Creates: 3 SAs (cloud-run-api, cloud-run-worker, pubsub-invoker)
  |     Creates: 11 google_project_iam_member bindings
  |     Outputs: api_sa_email, worker_sa_email, pubsub_invoker_sa_email
  |
  +-> module.networking
  |     Creates: VPC, /28 subnet, VPC Access connector
  |     Outputs: vpc_connector_id, vpc_id
  |
  +-> module.spanner  (depends on: module.iam)
  |     Creates: Spanner instance, database with DDL, database IAM binding
  |     Inputs:  api_sa_email, worker_sa_email from module.iam
  |     Outputs: instance_name, database_name
  |
  +-> module.storage  (depends on: module.iam)
  |     Creates: Document bucket, Artifact Registry repo, bucket IAM
  |     Inputs:  api_sa_email, worker_sa_email from module.iam
  |     Outputs: bucket_name, bucket_url, registry_url
  |
  +-> module.cloud_run_api  (depends on: module.iam, module.networking, module.spanner)
  |     Creates: Cloud Run v2 service (public ingress), allUsers IAM
  |     Inputs:  api_sa_email, vpc_connector_id, spanner instance/db names
  |     Outputs: service_url, service_name, service_id
  |
  +-> module.cloud_run_worker  (depends on: module.iam, module.networking, module.spanner)
  |     Creates: Cloud Run v2 service (internal ingress), SA-only IAM
  |     Inputs:  worker_sa_email, vpc_connector_id, spanner instance/db names
  |     Outputs: service_url, service_name, service_id
  |
  +-> module.pubsub  (depends on: module.iam, module.cloud_run_worker)
  |     Creates: Topic, DLQ topic, push subscription, DLQ pull subscription, IAM
  |     Inputs:  worker service_url, invoker_sa_email, worker_sa_email
  |     Outputs: topic_id, topic_name, subscription_id
  |
  +-> module.monitoring  (depends on: module.cloud_run_api)
        Creates: 3 alert policies, uptime check, email notification channel
        Inputs:  api service_name, api service_url, notification_email
        Outputs: notification_channel_id, alert_policy_ids
```

### Inter-Module Dependency Graph

```
                    +-------+
                    |  iam  |
                    +---+---+
                        |
          +-------------+-------------+-------------+
          |             |             |             |
          v             v             v             v
    +-----------+  +---------+  +---------+  +---------+
    | cloud-run |  | spanner |  | storage |  | pubsub  |
    | (api)     |  +---------+  +---------+  +----+----+
    +-----+-----+                                  ^
          |                                        |
          |     +-----------+                      |
          |     | cloud-run |----------------------+
          |     | (worker)  |  (service_url -> push_endpoint)
          |     +-----+-----+
          |           ^
          |           |
    +-----+-----+    |
    | networking +----+
    +-----+-----+   (vpc_connector_id)
          |
          v
    +-----+------+
    | monitoring |
    +------------+
    (api service_url -> uptime check)
```

### Bootstrap Sequence

```
Manual one-time execution:
  1. cd terraform/bootstrap/
  2. Fill in terraform.tfvars (project_id, region, github_org, github_repo)
  3. terraform init   (local state)
  4. terraform apply  (creates: state bucket, WIF pool, WIF provider, terraform SA)
  5. Note outputs: state_bucket_name, wif_provider_name, terraform_sa_email
  6. Configure GitHub repo:
     a. Settings > Secrets & Variables > Actions
     b. Set variable: WIF_PROVIDER = <wif_provider_name output>
     c. Set variable: TERRAFORM_SA = <terraform_sa_email output>
     d. Set variable: PROJECT_ID = <project_id>
     e. Create environments: "dev" (no protection), "prod" (require reviewers)
  7. cd ../environments/dev/ && terraform init && terraform plan
```

### CI/CD Pipeline Flow

```
PR opened/updated (paths: terraform/**)
  |
  +-> Job: plan (${{ matrix.environment }})  [dev, prod in parallel]
        1. Checkout
        2. google-github-actions/auth@v2 (WIF, id-token: write)
        3. hashicorp/setup-terraform@v3
        4. terraform init
        5. terraform fmt -check
        6. terraform validate
        7. terraform plan -out=tfplan
        8. Post plan as PR comment

Merge to main (paths: terraform/**)
  |
  +-> Job: apply (${{ matrix.environment }})
        environment: ${{ matrix.environment }}  (prod requires approval)
        1. Checkout
        2. google-github-actions/auth@v2
        3. hashicorp/setup-terraform@v3
        4. terraform init
        5. terraform apply -auto-approve
```

## File Changes

| # | File Path (absolute) | Action | Description |
|---|----------------------|--------|-------------|
| 1 | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/main.tf` | create | GCS state bucket (versioning, uniform access), WIF pool, WIF provider (OIDC, attribute_condition), Terraform SA, SA IAM member for WIF |
| 2 | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/variables.tf` | create | Variables: project_id, region (default "us-central1"), github_org, github_repo |
| 3 | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/outputs.tf` | create | Outputs: state_bucket_name, wif_provider_name, terraform_sa_email |
| 4 | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/versions.tf` | create | required_version >= 1.5, hashicorp/google ~> 6.0 provider |
| 5 | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/main.tf` | create | 3 SAs (cloud-run-api, cloud-run-worker, pubsub-invoker) with project, display_name, description; 11 google_project_iam_member bindings (5 API, 5 worker, 1 invoker) |
| 6 | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/variables.tf` | create | Variables: project_id, environment |
| 7 | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/outputs.tf` | create | Outputs: api_sa_email, worker_sa_email, pubsub_invoker_sa_email |
| 8 | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/versions.tf` | create | required_providers: hashicorp/google ~> 6.0 |
| 9 | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/main.tf` | create | google_project_service (vpcaccess API), google_compute_network (custom-mode), google_compute_subnetwork (/28), google_vpc_access_connector (e2-micro) |
| 10 | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/variables.tf` | create | Variables: project_id, region, environment |
| 11 | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/outputs.tf` | create | Outputs: vpc_connector_id, vpc_id |
| 12 | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/versions.tf` | create | required_providers: hashicorp/google ~> 6.0 |
| 13 | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/main.tf` | create | google_spanner_instance (regional, processing_units), google_spanner_database (DDL with Carriers + AppetiteRules, deletion_protection, version_retention_period), google_spanner_database_iam_binding (databaseUser for SAs) |
| 14 | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/variables.tf` | create | Variables: project_id, region, environment, spanner_processing_units, deletion_protection, sa_emails (list) |
| 15 | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/outputs.tf` | create | Outputs: instance_name, database_name |
| 16 | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/versions.tf` | create | required_providers: hashicorp/google ~> 6.0 |
| 17 | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | create | google_cloud_run_v2_service (parameterized name/location/ingress/deletion_protection, dynamic env + secret_env, vpc_access, scaling, resource limits), google_cloud_run_v2_service_iam_member (conditional: allUsers or SA-only based on allow_unauthenticated) |
| 18 | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/variables.tf` | create | Variables: service_name, region, project_id, image, min_instances, max_instances, env_vars, secret_env_vars, resource_limits, container_port, service_account_email, ingress, vpc_connector_id, vpc_egress, deletion_protection, allow_unauthenticated, invoker_sa_email, labels |
| 19 | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/outputs.tf` | create | Outputs: service_url, service_name, service_id |
| 20 | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/versions.tf` | create | required_providers: hashicorp/google ~> 6.0 |
| 21 | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/main.tf` | create | data.google_project (for project number), google_pubsub_topic (appetite-events), google_pubsub_topic (appetite-events-dlq), google_pubsub_subscription (push with OIDC, dead_letter_policy, retry_policy), google_pubsub_subscription (DLQ pull), IAM members (worker subscriber, API publisher, service agent DLQ publisher, service agent subscription subscriber) |
| 22 | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/variables.tf` | create | Variables: project_id, environment, push_endpoint, invoker_sa_email, worker_sa_email, api_sa_email |
| 23 | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/outputs.tf` | create | Outputs: topic_id, topic_name, subscription_id |
| 24 | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/versions.tf` | create | required_providers: hashicorp/google ~> 6.0 |
| 25 | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/main.tf` | create | google_storage_bucket (documents, uniform access, public_access_prevention, versioning, lifecycle rules), google_artifact_registry_repository (DOCKER format), google_storage_bucket_iam_member (objectUser for API + worker SAs) |
| 26 | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/variables.tf` | create | Variables: project_id, region, environment, api_sa_email, worker_sa_email |
| 27 | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/outputs.tf` | create | Outputs: bucket_name, bucket_url, registry_url |
| 28 | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/versions.tf` | create | required_providers: hashicorp/google ~> 6.0 |
| 29 | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/main.tf` | create | 3 google_monitoring_alert_policy (latency, error rate, CPU), google_monitoring_uptime_check_config (/health endpoint) |
| 30 | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/channels.tf` | create | google_monitoring_notification_channel (type=email, labels.email_address=var.notification_email) |
| 31 | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/variables.tf` | create | Variables: project_id, service_name, service_url, notification_email |
| 32 | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/outputs.tf` | create | Outputs: notification_channel_id, alert_policy_ids |
| 33 | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/versions.tf` | create | required_providers: hashicorp/google ~> 6.0 |
| 34 | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/backend.tf` | create | terraform backend "gcs" with prefix "environments/dev" |
| 35 | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/main.tf` | create | Provider config, module calls: iam, networking, spanner, cloud_run_api, cloud_run_worker, pubsub, storage, monitoring -- all wired with inter-module references, dev-specific values |
| 36 | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/variables.tf` | create | Variables: project_id, region, image_tag, ops_email |
| 37 | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/outputs.tf` | create | Outputs: api_url, worker_url, spanner_instance, spanner_database, pubsub_topic, bucket_name, registry_url |
| 38 | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/terraform.tfvars` | create | Dev values: project_id placeholder, region, image_tag, ops_email placeholder |
| 39 | `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/backend.tf` | create | terraform backend "gcs" with prefix "environments/prod" |
| 40 | `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/main.tf` | create | Same structure as dev with prod-specific values (min_instances=2, max_instances=20, 300 PU, deletion_protection=true) |
| 41 | `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/variables.tf` | create | Variables: project_id, region, image_tag, ops_email |
| 42 | `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/outputs.tf` | create | Same outputs as dev |
| 43 | `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/terraform.tfvars` | create | Prod values: project_id placeholder, region, image_tag, ops_email placeholder |
| 44 | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | create | CI/CD: WIF auth, matrix [dev,prod], plan-on-PR with comment, apply-on-merge with environment protection, fmt check, validate |

**Summary**: 44 files created, 0 files modified, 0 files deleted

## Interfaces and Contracts

### Module Variables and Outputs

#### bootstrap/variables.tf

```hcl
variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "region" {
  type        = string
  default     = "us-central1"
  description = "GCP region"
}

variable "github_org" {
  type        = string
  description = "GitHub organization name"
}

variable "github_repo" {
  type        = string
  description = "GitHub repository name"
}
```

#### bootstrap/outputs.tf

```hcl
output "state_bucket_name" {
  value       = google_storage_bucket.terraform_state.name
  description = "GCS bucket for Terraform remote state"
}

output "wif_provider_name" {
  value       = google_iam_workload_identity_pool_provider.github.name
  description = "Full resource name of the WIF provider"
}

output "terraform_sa_email" {
  value       = google_service_account.terraform.email
  description = "Terraform automation service account email"
}
```

#### modules/iam/variables.tf

```hcl
variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "environment" {
  type        = string
  description = "Environment name (dev, prod)"
}
```

#### modules/iam/outputs.tf

```hcl
output "api_sa_email" {
  value       = google_service_account.cloud_run_api.email
  description = "Cloud Run API service account email"
}

output "worker_sa_email" {
  value       = google_service_account.cloud_run_worker.email
  description = "Cloud Run worker service account email"
}

output "pubsub_invoker_sa_email" {
  value       = google_service_account.pubsub_invoker.email
  description = "Pub/Sub push invoker service account email"
}
```

#### modules/networking/variables.tf

```hcl
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
```

#### modules/networking/outputs.tf

```hcl
output "vpc_connector_id" {
  value       = google_vpc_access_connector.connector.id
  description = "VPC Access connector ID for Cloud Run"
}

output "vpc_id" {
  value       = google_compute_network.vpc.id
  description = "VPC network ID"
}
```

#### modules/spanner/variables.tf

```hcl
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
```

#### modules/spanner/outputs.tf

```hcl
output "instance_name" {
  value       = google_spanner_instance.main.name
  description = "Spanner instance name"
}

output "database_name" {
  value       = google_spanner_database.main.name
  description = "Spanner database name"
}
```

#### modules/cloud-run/variables.tf

```hcl
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
  type = list(object({
    name  = string
    value = string
  }))
  default     = []
  description = "Plain-text environment variables"
}

variable "secret_env_vars" {
  type = list(object({
    name    = string
    secret  = string
    version = string
  }))
  default     = []
  description = "Secret Manager-backed environment variables"
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
```

#### modules/cloud-run/outputs.tf

```hcl
output "service_url" {
  value       = google_cloud_run_v2_service.service.uri
  description = "Cloud Run service HTTPS URL"
}

output "service_name" {
  value       = google_cloud_run_v2_service.service.name
  description = "Cloud Run service name"
}

output "service_id" {
  value       = google_cloud_run_v2_service.service.id
  description = "Cloud Run service ID"
}
```

#### modules/pubsub/variables.tf

```hcl
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
```

#### modules/pubsub/outputs.tf

```hcl
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
```

#### modules/storage/variables.tf

```hcl
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
```

#### modules/storage/outputs.tf

```hcl
output "bucket_name" {
  value       = google_storage_bucket.documents.name
  description = "Document storage bucket name"
}

output "bucket_url" {
  value       = google_storage_bucket.documents.url
  description = "Document storage bucket URL"
}

output "registry_url" {
  value       = "${var.region}-docker.pkg.dev/${var.project_id}/${google_artifact_registry_repository.docker.repository_id}"
  description = "Artifact Registry Docker repository URL"
}
```

#### modules/monitoring/variables.tf

```hcl
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
```

#### modules/monitoring/outputs.tf

```hcl
output "notification_channel_id" {
  value       = google_monitoring_notification_channel.email.id
  description = "Email notification channel ID"
}

output "alert_policy_ids" {
  value = [
    google_monitoring_alert_policy.latency.id,
    google_monitoring_alert_policy.error_rate.id,
    google_monitoring_alert_policy.cpu_utilization.id,
  ]
  description = "Alert policy IDs"
}
```

#### environments/dev/variables.tf and environments/prod/variables.tf

```hcl
variable "project_id" {
  type        = string
  description = "GCP project ID"
}

variable "region" {
  type        = string
  default     = "us-central1"
  description = "GCP region"
}

variable "image_tag" {
  type        = string
  default     = "latest"
  description = "Container image tag"
}

variable "ops_email" {
  type        = string
  description = "Operations team email for alert notifications"
}
```

### Environment-Specific Values

#### terraform/environments/dev/terraform.tfvars

```hcl
# project_id = "your-dev-project-id"    # Set via CLI or CI/CD: -var="project_id=..."
# ops_email  = "dev-ops@example.com"     # Set via CLI or CI/CD
region    = "us-central1"
image_tag = "latest"
```

#### terraform/environments/prod/terraform.tfvars

```hcl
# project_id = "your-prod-project-id"   # Set via CLI or CI/CD: -var="project_id=..."
# ops_email  = "prod-ops@example.com"    # Set via CLI or CI/CD
region    = "us-central1"
image_tag = "latest"
```

### Environment Module Wiring (dev main.tf -- prod identical except values)

| Module Call | Key Input | Source | Dev Value |
|---|---|---|---|
| `module.iam` | `project_id` | `var.project_id` | from tfvars |
| `module.iam` | `environment` | literal | `"dev"` |
| `module.networking` | `project_id` | `var.project_id` | from tfvars |
| `module.networking` | `region` | `var.region` | `"us-central1"` |
| `module.networking` | `environment` | literal | `"dev"` |
| `module.spanner` | `spanner_processing_units` | literal | `100` (prod: `300`) |
| `module.spanner` | `deletion_protection` | literal | `false` (prod: `true`) |
| `module.spanner` | `sa_emails` | `module.iam` | `[module.iam.api_sa_email, module.iam.worker_sa_email]` |
| `module.cloud_run_api` | `service_name` | literal | `"appetite-api"` |
| `module.cloud_run_api` | `image` | expression | `"${var.region}-docker.pkg.dev/${var.project_id}/appetite-engine/api:${var.image_tag}"` |
| `module.cloud_run_api` | `min_instances` | literal | `0` (prod: `2`) |
| `module.cloud_run_api` | `max_instances` | literal | `5` (prod: `20`) |
| `module.cloud_run_api` | `ingress` | literal | `"INGRESS_TRAFFIC_ALL"` |
| `module.cloud_run_api` | `allow_unauthenticated` | literal | `true` |
| `module.cloud_run_api` | `deletion_protection` | literal | `false` (prod: `true`) |
| `module.cloud_run_api` | `service_account_email` | `module.iam` | `module.iam.api_sa_email` |
| `module.cloud_run_api` | `vpc_connector_id` | `module.networking` | `module.networking.vpc_connector_id` |
| `module.cloud_run_api` | `env_vars` | expression | `[{name="PROJECT_ID", value=var.project_id}, {name="SPANNER_INSTANCE", value=module.spanner.instance_name}, {name="SPANNER_DATABASE", value=module.spanner.database_name}]` |
| `module.cloud_run_worker` | `service_name` | literal | `"appetite-worker"` |
| `module.cloud_run_worker` | `ingress` | literal | `"INGRESS_TRAFFIC_INTERNAL_ONLY"` |
| `module.cloud_run_worker` | `allow_unauthenticated` | literal | `false` |
| `module.cloud_run_worker` | `invoker_sa_email` | `module.iam` | `module.iam.pubsub_invoker_sa_email` |
| `module.cloud_run_worker` | `service_account_email` | `module.iam` | `module.iam.worker_sa_email` |
| `module.pubsub` | `push_endpoint` | `module.cloud_run_worker` | `"${module.cloud_run_worker.service_url}/events"` |
| `module.pubsub` | `invoker_sa_email` | `module.iam` | `module.iam.pubsub_invoker_sa_email` |
| `module.pubsub` | `worker_sa_email` | `module.iam` | `module.iam.worker_sa_email` |
| `module.pubsub` | `api_sa_email` | `module.iam` | `module.iam.api_sa_email` |
| `module.storage` | `api_sa_email` | `module.iam` | `module.iam.api_sa_email` |
| `module.storage` | `worker_sa_email` | `module.iam` | `module.iam.worker_sa_email` |
| `module.monitoring` | `service_name` | `module.cloud_run_api` | `module.cloud_run_api.service_name` |
| `module.monitoring` | `service_url` | `module.cloud_run_api` | `module.cloud_run_api.service_url` |
| `module.monitoring` | `notification_email` | `var.ops_email` | from tfvars |

### Key Resources per Module

#### bootstrap/main.tf

| Resource Type | Terraform Name | Key Arguments |
|---|---|---|
| `google_storage_bucket` | `terraform_state` | `name="${var.project_id}-terraform-state"`, `versioning.enabled=true`, `uniform_bucket_level_access=true`, `force_destroy=false` |
| `google_iam_workload_identity_pool` | `github` | `workload_identity_pool_id="github-actions"`, `display_name="GitHub Actions"` |
| `google_iam_workload_identity_pool_provider` | `github` | `issuer_uri="https://token.actions.githubusercontent.com"`, `attribute_condition` restricting to `var.github_org/var.github_repo`, `attribute_mapping` for subject/actor/repository/ref |
| `google_service_account` | `terraform` | `account_id="terraform"`, `display_name="Terraform Automation"` |
| `google_service_account_iam_member` | `wif_terraform` | `role="roles/iam.workloadIdentityUser"`, `member="principalSet://..."` |

#### modules/iam/main.tf

| Resource Type | Terraform Name | Key Arguments |
|---|---|---|
| `google_service_account` | `cloud_run_api` | `account_id="cloud-run-api-${var.environment}"` |
| `google_service_account` | `cloud_run_worker` | `account_id="cloud-run-worker-${var.environment}"` |
| `google_service_account` | `pubsub_invoker` | `account_id="pubsub-invoker-${var.environment}"` |
| `google_project_iam_member` | `api_spanner` | `role="roles/spanner.databaseUser"`, member=API SA |
| `google_project_iam_member` | `api_secretmanager` | `role="roles/secretmanager.secretAccessor"`, member=API SA |
| `google_project_iam_member` | `api_storage` | `role="roles/storage.objectUser"`, member=API SA |
| `google_project_iam_member` | `api_logging` | `role="roles/logging.logWriter"`, member=API SA |
| `google_project_iam_member` | `api_tracing` | `role="roles/cloudtrace.agent"`, member=API SA |
| `google_project_iam_member` | `worker_spanner` | `role="roles/spanner.databaseUser"`, member=worker SA |
| `google_project_iam_member` | `worker_secretmanager` | `role="roles/secretmanager.secretAccessor"`, member=worker SA |
| `google_project_iam_member` | `worker_logging` | `role="roles/logging.logWriter"`, member=worker SA |
| `google_project_iam_member` | `worker_tracing` | `role="roles/cloudtrace.agent"`, member=worker SA |
| `google_project_iam_member` | `worker_pubsub` | `role="roles/pubsub.subscriber"`, member=worker SA |
| `google_project_iam_member` | `invoker_run` | `role="roles/run.invoker"`, member=invoker SA |

#### modules/networking/main.tf

| Resource Type | Terraform Name | Key Arguments |
|---|---|---|
| `google_project_service` | `vpcaccess` | `service="vpcaccess.googleapis.com"`, `disable_on_destroy=false` |
| `google_compute_network` | `vpc` | `name="appetite-vpc-${var.environment}"`, `auto_create_subnetworks=false` |
| `google_compute_subnetwork` | `connector_subnet` | `name="vpc-connector-subnet-${var.environment}"`, `ip_cidr_range="10.8.0.0/28"`, `region=var.region` |
| `google_vpc_access_connector` | `connector` | `name="vpc-connector-${var.environment}"`, `subnet.name` referencing connector_subnet, `machine_type="e2-micro"`, `min_instances=2`, `max_instances=10` |

#### modules/spanner/main.tf

| Resource Type | Terraform Name | Key Arguments |
|---|---|---|
| `google_spanner_instance` | `main` | `name="appetite-${var.environment}"`, `config="regional-${var.region}"`, `processing_units=var.spanner_processing_units`, `force_destroy=false` |
| `google_spanner_database` | `main` | `instance` ref, `name="appetite-db"`, `ddl` (Carriers, AppetiteRules), `deletion_protection=var.deletion_protection`, `version_retention_period="3d"` |
| `google_spanner_database_iam_binding` | `database_user` | `role="roles/spanner.databaseUser"`, `members` from var.sa_emails |

#### modules/cloud-run/main.tf

| Resource Type | Terraform Name | Key Arguments |
|---|---|---|
| `google_cloud_run_v2_service` | `service` | `name=var.service_name`, `location=var.region`, `ingress=var.ingress`, `deletion_protection=var.deletion_protection`, template with scaling/containers/env/vpc_access |
| `google_cloud_run_v2_service_iam_member` | `public` | count based on `var.allow_unauthenticated`, `role="roles/run.invoker"`, `member="allUsers"` |
| `google_cloud_run_v2_service_iam_member` | `invoker` | count based on `!var.allow_unauthenticated && var.invoker_sa_email != ""`, `role="roles/run.invoker"`, `member="serviceAccount:${var.invoker_sa_email}"` |

#### modules/pubsub/main.tf

| Resource Type | Terraform Name | Key Arguments |
|---|---|---|
| `data.google_project` | `current` | `project_id=var.project_id` |
| `google_pubsub_topic` | `appetite_events` | `name="appetite-events-${var.environment}"`, `message_retention_duration="86400s"` |
| `google_pubsub_topic` | `appetite_events_dlq` | `name="appetite-events-dlq-${var.environment}"` |
| `google_pubsub_subscription` | `appetite_events_push` | `topic` ref, `push_config.push_endpoint=var.push_endpoint`, `oidc_token.service_account_email=var.invoker_sa_email`, `dead_letter_policy` ref to DLQ topic, `retry_policy` |
| `google_pubsub_subscription` | `appetite_events_dlq_pull` | `topic` ref to DLQ, no push_config (pull mode) |
| `google_pubsub_subscription_iam_member` | `worker_subscriber` | `role="roles/pubsub.subscriber"`, member=worker SA |
| `google_pubsub_topic_iam_member` | `api_publisher` | `role="roles/pubsub.publisher"`, member=API SA |
| `google_pubsub_topic_iam_member` | `dlq_publisher` | `role="roles/pubsub.publisher"` on DLQ topic, member=service agent |
| `google_pubsub_subscription_iam_member` | `dlq_subscriber` | `role="roles/pubsub.subscriber"` on push subscription, member=service agent |

#### modules/storage/main.tf

| Resource Type | Terraform Name | Key Arguments |
|---|---|---|
| `google_storage_bucket` | `documents` | `name="${var.project_id}-documents-${var.environment}"`, `uniform_bucket_level_access=true`, `public_access_prevention="enforced"`, `versioning.enabled=true`, lifecycle rules (Nearline@30d, Coldline@90d, old version cleanup) |
| `google_artifact_registry_repository` | `docker` | `repository_id="appetite-engine"`, `format="DOCKER"`, `location=var.region` |
| `google_storage_bucket_iam_member` | `api_objectuser` | `role="roles/storage.objectUser"`, member=API SA |
| `google_storage_bucket_iam_member` | `worker_objectuser` | `role="roles/storage.objectUser"`, member=worker SA |

#### modules/monitoring/main.tf

| Resource Type | Terraform Name | Key Arguments |
|---|---|---|
| `google_monitoring_alert_policy` | `latency` | filter: `run.googleapis.com/request_latencies`, service_name from var, `threshold_value=2000`, `alignment=ALIGN_PERCENTILE_99` |
| `google_monitoring_alert_policy` | `error_rate` | filter: `run.googleapis.com/request_count` + `response_code_class="5xx"`, `threshold_value=0.05` |
| `google_monitoring_alert_policy` | `cpu_utilization` | filter: `run.googleapis.com/container/cpu/utilizations`, `threshold_value=0.8` |
| `google_monitoring_uptime_check_config` | `api_health` | `http_check.path="/health"`, `port=443`, `use_ssl=true`, `host=trimprefix(var.service_url, "https://")` |

#### modules/monitoring/channels.tf

| Resource Type | Terraform Name | Key Arguments |
|---|---|---|
| `google_monitoring_notification_channel` | `email` | `type="email"`, `labels.email_address=var.notification_email` |

### Database Schema

#### Spanner DDL (embedded in modules/spanner/main.tf via `google_spanner_database.main.ddl`)

```sql
-- DDL statement 1: Carriers table
CREATE TABLE Carriers (
  CarrierId     STRING(36) NOT NULL,
  Name          STRING(256) NOT NULL,
  Status        STRING(20) NOT NULL,
  CreatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
  UpdatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
) PRIMARY KEY(CarrierId)

-- DDL statement 2: AppetiteRules table (interleaved in Carriers)
CREATE TABLE AppetiteRules (
  CarrierId     STRING(36) NOT NULL,
  RuleId        STRING(36) NOT NULL,
  LineOfBusiness STRING(100) NOT NULL,
  State         STRING(2) NOT NULL,
  MinPremium    FLOAT64,
  MaxPremium    FLOAT64,
  IsActive      BOOL NOT NULL,
  CreatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
  UpdatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
) PRIMARY KEY(CarrierId, RuleId),
  INTERLEAVE IN PARENT Carriers ON DELETE CASCADE

-- DDL statement 3: Index for AppetiteRules by line of business
CREATE INDEX AppetiteRulesByLoB ON AppetiteRules(LineOfBusiness)

-- DDL statement 4: Index for AppetiteRules by state
CREATE INDEX AppetiteRulesByState ON AppetiteRules(State)
```

In HCL, these are provided as a list of strings in the `ddl` argument:

```hcl
ddl = [
  <<-EOT
    CREATE TABLE Carriers (
      CarrierId     STRING(36) NOT NULL,
      Name          STRING(256) NOT NULL,
      Status        STRING(20) NOT NULL,
      CreatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
      UpdatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
    ) PRIMARY KEY(CarrierId)
  EOT
  ,
  <<-EOT
    CREATE TABLE AppetiteRules (
      CarrierId     STRING(36) NOT NULL,
      RuleId        STRING(36) NOT NULL,
      LineOfBusiness STRING(100) NOT NULL,
      State         STRING(2) NOT NULL,
      MinPremium    FLOAT64,
      MaxPremium    FLOAT64,
      IsActive      BOOL NOT NULL,
      CreatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
      UpdatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
    ) PRIMARY KEY(CarrierId, RuleId),
      INTERLEAVE IN PARENT Carriers ON DELETE CASCADE
  EOT
  ,
  "CREATE INDEX AppetiteRulesByLoB ON AppetiteRules(LineOfBusiness)",
  "CREATE INDEX AppetiteRulesByState ON AppetiteRules(State)",
]
```

### CI/CD Workflow Contract

```yaml
# .github/workflows/terraform.yml
name: Terraform

on:
  pull_request:
    branches: [main]
    paths:
      - 'terraform/**'
      - '.github/workflows/terraform.yml'
  push:
    branches: [main]
    paths:
      - 'terraform/**'
      - '.github/workflows/terraform.yml'

permissions:
  contents: read
  pull-requests: write
  id-token: write

jobs:
  plan:
    name: Plan (${{ matrix.environment }})
    runs-on: ubuntu-latest
    if: github.event_name == 'pull_request'
    strategy:
      matrix:
        environment: [dev, prod]
      fail-fast: false
    steps:
      # 1. actions/checkout@v4
      # 2. google-github-actions/auth@v2 (WIF)
      # 3. hashicorp/setup-terraform@v3
      # 4. terraform init
      # 5. terraform fmt -check -recursive
      # 6. terraform validate
      # 7. terraform plan -out=tfplan
      # 8. PR comment with plan output

  apply:
    name: Apply (${{ matrix.environment }})
    runs-on: ubuntu-latest
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    environment: ${{ matrix.environment }}
    strategy:
      matrix:
        environment: [dev, prod]
      fail-fast: false
    steps:
      # 1. actions/checkout@v4
      # 2. google-github-actions/auth@v2 (WIF)
      # 3. hashicorp/setup-terraform@v3
      # 4. terraform init
      # 5. terraform apply -auto-approve

# GitHub repo configuration (manual):
#   vars.WIF_PROVIDER = projects/<number>/locations/global/workloadIdentityPools/github-actions/providers/github-provider
#   vars.TERRAFORM_SA = terraform@<project-id>.iam.gserviceaccount.com
#   vars.PROJECT_ID   = <project-id>
#   environments: dev (no protection), prod (require reviewers)
```

## Testing Strategy

| # | What to Test | Type | File Path | Maps to Requirement |
|---|---|---|---|---|
| 1 | Bootstrap state bucket configuration (versioning, uniform access, force_destroy) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/main.tf` | REQ-BOOT-001 |
| 2 | WIF pool and provider creation with attribute_condition | code-based | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/main.tf` | REQ-BOOT-002 |
| 3 | Terraform SA with WIF binding, no key resource | code-based | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/main.tf` | REQ-BOOT-003 |
| 4 | Bootstrap module structure (4 files, versions.tf content) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/` | REQ-BOOT-004 |
| 5 | Bootstrap outputs declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/outputs.tf` | REQ-BOOT-005 |
| 6 | Bootstrap variables declared, no hardcoded values | code-based | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/variables.tf` | REQ-BOOT-006 |
| 7 | 3 SAs created with correct account_ids | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/main.tf` | REQ-IAM-001 |
| 8 | Only additive IAM member resources, correct roles per SA | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/main.tf` | REQ-IAM-002 |
| 9 | IAM outputs declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/outputs.tf` | REQ-IAM-003 |
| 10 | IAM module structure | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/` | REQ-IAM-004 |
| 11 | No hardcoded values in IAM | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/` | REQ-IAM-005 |
| 12 | Custom-mode VPC created | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/main.tf` | REQ-NET-001 |
| 13 | /28 subnet with parameterized region | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/main.tf` | REQ-NET-002 |
| 14 | VPC Access connector with e2-micro | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/main.tf` | REQ-NET-003 |
| 15 | Networking outputs declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/outputs.tf` | REQ-NET-004 |
| 16 | Networking module structure | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/` | REQ-NET-005 |
| 17 | VPC Access API enabled | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/main.tf` | REQ-NET-006 |
| 18 | Spanner instance with processing_units, regional config | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/main.tf` | REQ-SPAN-001 |
| 19 | Database DDL with Carriers and AppetiteRules tables | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/main.tf` | REQ-SPAN-002 |
| 20 | Database IAM binding for SA emails | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/main.tf` | REQ-SPAN-003 |
| 21 | Spanner outputs declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/outputs.tf` | REQ-SPAN-004 |
| 22 | Spanner module structure | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/` | REQ-SPAN-005 |
| 23 | Spanner variables declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/variables.tf` | REQ-SPAN-006 |
| 24 | Cloud Run v2 service resource, parameterized name/location | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | REQ-CRUN-001 |
| 25 | Scaling from variable inputs | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | REQ-CRUN-002 |
| 26 | Container image parameterized | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | REQ-CRUN-003 |
| 27 | Dynamic env blocks for plain and secret vars | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | REQ-CRUN-004 |
| 28 | VPC connector wired from variable | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | REQ-CRUN-005 |
| 29 | Resource-level IAM (allUsers vs SA-only) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | REQ-CRUN-006 |
| 30 | Cloud Run outputs declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/outputs.tf` | REQ-CRUN-007 |
| 31 | Cloud Run module structure | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/` | REQ-CRUN-008 |
| 32 | Service account assigned from variable | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | REQ-CRUN-009 |
| 33 | Ingress parameterized | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | REQ-CRUN-010 |
| 34 | Appetite events topic created | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/main.tf` | REQ-PSUB-001 |
| 35 | DLQ topic created | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/main.tf` | REQ-PSUB-002 |
| 36 | Push subscription with OIDC, dead-letter, retry | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/main.tf` | REQ-PSUB-003 |
| 37 | DLQ pull subscription (no push_config) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/main.tf` | REQ-PSUB-004 |
| 38 | Pub/Sub IAM (worker subscriber, service agent DLQ) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/main.tf` | REQ-PSUB-005 |
| 39 | Pub/Sub outputs declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/outputs.tf` | REQ-PSUB-006 |
| 40 | Pub/Sub module structure | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/` | REQ-PSUB-007 |
| 41 | Document bucket with security settings and versioning | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/main.tf` | REQ-STOR-001 |
| 42 | Lifecycle rules (Nearline transition, old version cleanup) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/main.tf` | REQ-STOR-002 |
| 43 | Artifact Registry repo (DOCKER, parameterized region) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/main.tf` | REQ-STOR-003 |
| 44 | Bucket IAM (objectUser, no public access) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/main.tf` | REQ-STOR-004 |
| 45 | Storage outputs declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/outputs.tf` | REQ-STOR-005 |
| 46 | Storage module structure | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/` | REQ-STOR-006 |
| 47 | Email notification channel in channels.tf, not main.tf | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/channels.tf` | REQ-MON-001 |
| 48 | Latency alert policy with notification channel ref | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/main.tf` | REQ-MON-002 |
| 49 | Error rate alert policy (5xx filter) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/main.tf` | REQ-MON-003 |
| 50 | CPU utilization alert policy (threshold 0.8) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/main.tf` | REQ-MON-004 |
| 51 | Uptime check (/health, port 443, SSL, host from var) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/main.tf` | REQ-MON-005 |
| 52 | Monitoring module structure (5 files including channels.tf) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/` | REQ-MON-006 |
| 53 | Monitoring outputs declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/outputs.tf` | REQ-MON-007 |
| 54 | Monitoring variables declared | code-based | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/variables.tf` | REQ-MON-008 |
| 55 | Dev and prod backend prefixes isolated | code-based | `/home/reche/projects/ProyectoAgentero/terraform/environments/*/backend.tf` | REQ-ENV-001 |
| 56 | All modules instantiated in env main.tf | code-based | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/main.tf` | REQ-ENV-002 |
| 57 | Inter-module wiring (VPC connector, SA emails, push endpoint) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/main.tf` | REQ-ENV-003 |
| 58 | Dev parameterization (100 PU, scale-to-zero, no deletion protection) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/main.tf` | REQ-ENV-004 |
| 59 | Prod parameterization (300 PU, min 2, deletion protection) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/main.tf` | REQ-ENV-005 |
| 60 | API public ingress, worker internal-only | code-based | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/main.tf` | REQ-ENV-006 |
| 61 | Environment directory structure (5 files each) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/environments/` | REQ-ENV-007 |
| 62 | Environment-level outputs (service URLs, Spanner names) | code-based | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/outputs.tf` | REQ-ENV-008 |
| 63 | terraform validate passes (dev + prod) | validation | `/home/reche/projects/ProyectoAgentero/terraform/environments/` | REQ-ENV-009 |
| 64 | No hardcoded values in environments | code-based | `/home/reche/projects/ProyectoAgentero/terraform/environments/` | REQ-ENV-010 |
| 65 | Workflow triggers on terraform path changes, push to main | code-based | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | REQ-CICD-001 |
| 66 | WIF auth action used, no stored credentials, id-token permission | code-based | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | REQ-CICD-002 |
| 67 | Matrix strategy includes dev and prod | code-based | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | REQ-CICD-003 |
| 68 | Plan on PR with init before plan | code-based | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | REQ-CICD-004 |
| 69 | Apply only on push to main with -auto-approve | code-based | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | REQ-CICD-005 |
| 70 | Workflow references environment for protection | code-based | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | REQ-CICD-006 |
| 71 | Terraform setup action used | code-based | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | REQ-CICD-007 |
| 72 | Format check and validate steps | code-based | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | REQ-CICD-008 |
| 73 | No hardcoded credentials in workflow | code-based | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | REQ-CICD-009 |

### Test Dependencies

- **Mocks needed**: None. All tests are code-based (file inspection / HCL validation).
- **Fixtures needed**: None. Terraform files are self-contained declarative configurations.
- **Infrastructure**: Terraform CLI >= 1.5 required for `terraform init -backend=false && terraform validate`. No GCP project needed for validation.

## Migration and Rollout

### Bootstrap Steps (one-time, manual)

1. Ensure a GCP project exists with billing enabled
2. Authenticate: `gcloud auth application-default login`
3. `cd terraform/bootstrap/`
4. Create `terraform.tfvars` with project_id, github_org, github_repo
5. `terraform init`
6. `terraform apply` -- creates state bucket, WIF pool/provider, Terraform SA
7. Note the outputs
8. Grant the Terraform SA necessary project-level roles:
   - `roles/editor` (or granular roles for each resource type)
   - `roles/iam.securityAdmin` (for managing IAM bindings)
9. Configure GitHub repository secrets/variables (WIF_PROVIDER, TERRAFORM_SA, PROJECT_ID)
10. Create GitHub environments: "dev" (no protection), "prod" (require reviewers)

### Environment Deployment

1. `cd terraform/environments/dev/`
2. `terraform init` (uses GCS backend created by bootstrap)
3. `terraform plan -var="project_id=..." -var="ops_email=..."`
4. `terraform apply` (creates all dev infrastructure)
5. Repeat for prod (with manual approval gate if via CI/CD)

### Rollback Steps

This change creates only new files. No existing code is modified.

- **Code rollback**: Delete `terraform/` directory and `.github/workflows/terraform.yml`
- **Infrastructure rollback** (if `terraform apply` was run):
  1. `cd terraform/environments/prod/ && terraform destroy` (if prod was applied)
  2. `cd terraform/environments/dev/ && terraform destroy` (if dev was applied)
  3. `cd terraform/bootstrap/ && terraform destroy` (removes state bucket, WIF, SA)
- **No infrastructure exists until `terraform apply` is explicitly run**, so file deletion is sufficient if only code was committed

## Open Questions

- **Spanner DDL schema**: The domain models (Carrier, AppetiteRule) are documented in CLAUDE.md but no Go source files exist yet. The DDL in this design uses reasonable column types based on the domain description (insurtech appetite matching). The final schema should be validated against the actual Go domain types when they are implemented. If the Go domain layer is implemented first, the DDL should be updated to match.
- **project_id and ops_email in tfvars**: These are commented out in terraform.tfvars to avoid committing project-specific values. They should be provided via `-var` flags or CI/CD variables. An alternative is to use a `terraform.tfvars.example` pattern and `.gitignore` the actual tfvars.

---

**Next Step**: After both design and specs are complete, run `sdd-tasks` to create the implementation checklist.
