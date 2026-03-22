# Technical Design: OWASP Security Hardening

**Change**: owasp-security-hardening
**Date**: 2026-03-22T16:00:00Z
**Status**: draft
**Depends On**: proposal.md, specs/spec.md

---

## Technical Approach

All fixes are in-place modifications to existing Terraform modules and the CI/CD workflow — no new structural layers are introduced beyond the optional KMS module (gated by `enable_cmek = false`). Each module receives only the resources and variables that belong to its domain: audit config stays in `iam/`, firewall rules in `networking/`, registry IAM in `storage/`, worker alerts in `monitoring/`, and Terraform SA roles in `bootstrap/`. This avoids cross-module coupling and means every fix can be reviewed and applied independently.

The implementation order is bootstrap first (isolated root, no environment dependency), then modules (no impact until re-applied), then environment wiring (triggers apply on next push), then CI/CD workflow (no infrastructure impact). This order keeps blast radius minimal at each step.

Variable additions to the monitoring module use `default = ""` with a `count = var.worker_service_name != "" ? 1 : 0` guard on new resources, preserving backward compatibility with callers that only pass API variables.

---

## Architecture Decisions

| # | Decision | Choice | Alternatives Considered | Rationale |
|---|----------|--------|------------------------|-----------|
| 1 | Audit log placement | `google_project_iam_audit_config` in `modules/iam/main.tf` | New `modules/iam/audit.tf` sibling file | Single file keeps the IAM module coherent; adding a sibling file is premature for one resource |
| 2 | Worker monitoring pattern | Add `worker_service_name` / `worker_service_url` optional vars; mirror API alert resources with `count` guard | Instantiate monitoring module twice in each env | Optional vars are backward compatible; two module calls would duplicate the notification channel resource and conflict |
| 3 | Firewall egress target | Allow `199.36.153.8/30` (restricted.googleapis.com) for GCP private API egress | Allow `0.0.0.0/0` egress, use VPC Service Controls | Restricted API range is the minimal allow needed for Spanner/Pub/Sub/GCS via VPC; `0.0.0.0/0` defeats the deny-all; VPC-SC is out of scope |
| 4 | CI/CD plan masking | `terraform show -no-color tfplan \| grep -E '^\s*(#\|~\|+\|-)' \| head -100` piped to a file, file content in PR comment | Strip with external redaction tool, post only counts | No external tooling needed; grep on known terraform output prefixes removes data values while keeping change summary |
| 5 | Artifact Registry IAM placement | `google_artifact_registry_repository_iam_member` in `modules/storage/main.tf` | In `modules/iam/main.tf` as project-scoped | Resource-scoped IAM belongs next to the resource it protects; `storage` module already owns the AR repository resource |
| 6 | VPC connector machine_type | New `connector_machine_type` variable with `default = "e2-micro"` | Hard-code `e2-standard-4` everywhere, or separate variable per env | Variable with default preserves dev behavior; prod tfvars overrides to `e2-standard-4` |
| 7 | Provider version pin | `~> 6.14.1` across all 8 `versions.tf` | Exact `= "6.14.1"`, or keep `~> 6.0` | Patch-pessimistic allows patch-level security updates without lockfile changes; exact pin is too restrictive for routine patches; `~> 6.0` is the current insecure state |

---

## Data Flow

### Audit Logging

```
Any GCP API call (admin, data read, data write)
  -> GCP AuditLog subsystem
    -> Cloud Logging (DATA_READ, DATA_WRITE, ADMIN_READ for allServices)
      <- Enforced by google_project_iam_audit_config in iam/main.tf
         (project-level; applies to all service accounts and users)
```

### Firewall Evaluation (inbound to VPC connector subnet)

```
Ingress traffic
  -> google_compute_firewall.allow_health_checks  (priority 1000, src 35.191.0.0/16, 130.211.0.0/22)
  -> google_compute_firewall.allow_internal        (priority 1000, src 10.8.0.0/28)
  -> google_compute_firewall.deny_all_ingress      (priority 65534, src 0.0.0.0/0) — default deny

Egress traffic from VPC connector subnet
  -> google_compute_firewall.allow_gcp_apis        (priority 1000, dst 199.36.153.8/30, port 443)
  -> google_compute_firewall.deny_all_egress        (priority 65534, dst 0.0.0.0/0) — default deny
```

### Terraform SA Role Resolution (bootstrap)

```
CI/CD github-actions runner (WIF OIDC token)
  -> google_service_account_iam_member.wif_terraform (workloadIdentityUser)
    -> google_service_account.terraform
      -> google_project_iam_member.terraform_roles[*]
         (11 explicit roles — no roles/editor)
```

### CI/CD Plan Output (masked)

```
terraform plan -no-color -out=tfplan        (plan step, no continue-on-error)
  -> tfplan binary artifact
    -> terraform show -no-color tfplan       (show step)
      | grep -E '^\s*(#|~|\+|-)' | head -100
        -> plan_summary.txt                  (contains only change lines, no values)
          -> actions/github-script           (posts plan_summary.txt content to PR comment)
Plan Status step: if plan.outcome == 'failure' -> exit 1   (still present; now reachable)
```

---

## File Changes

| # | File Path | Action | Description |
|---|-----------|--------|-------------|
| 1 | `terraform/modules/iam/main.tf` | modify | Add `google_project_iam_audit_config`; remove `roles/secretmanager.secretAccessor` from `sa_project_roles`; add comment |
| 2 | `terraform/modules/iam/versions.tf` | modify | Pin provider to `~> 6.14.1` |
| 3 | `terraform/modules/networking/main.tf` | modify | Add 4 `google_compute_firewall` resources; add `google_compute_security_policy` (Cloud Armor placeholder); change `machine_type` to `var.connector_machine_type` |
| 4 | `terraform/modules/networking/variables.tf` | modify | Add `connector_machine_type` variable |
| 5 | `terraform/modules/networking/versions.tf` | modify | Pin provider to `~> 6.14.1` |
| 6 | `terraform/modules/storage/main.tf` | modify | Add 2 `google_artifact_registry_repository_iam_member` resources (api reader, worker reader); add `vulnerability_scanning_config` to AR repo |
| 7 | `terraform/modules/storage/variables.tf` | modify | No new variables needed (api_sa_email and worker_sa_email already present) |
| 8 | `terraform/modules/storage/versions.tf` | modify | Pin provider to `~> 6.14.1` |
| 9 | `terraform/modules/monitoring/main.tf` | modify | Add worker latency, error rate, CPU alert resources (guarded by `count`); add Pub/Sub DLQ alert |
| 10 | `terraform/modules/monitoring/variables.tf` | modify | Add `worker_service_name`, `worker_service_url`, `pubsub_subscription_name` optional variables |
| 11 | `terraform/modules/monitoring/versions.tf` | modify | Pin provider to `~> 6.14.1` |
| 12 | `terraform/modules/spanner/versions.tf` | modify | Pin provider to `~> 6.14.1` |
| 13 | `terraform/modules/pubsub/versions.tf` | modify | Pin provider to `~> 6.14.1` |
| 14 | `terraform/modules/cloud-run/versions.tf` | modify | Pin provider to `~> 6.14.1` |
| 15 | `terraform/bootstrap/main.tf` | modify | Add `public_access_prevention = "enforced"` to state bucket; add 11 `google_project_iam_member.terraform_roles` resources |
| 16 | `terraform/bootstrap/versions.tf` | modify | Pin provider to `~> 6.14.1` |
| 17 | `terraform/environments/dev/main.tf` | modify | Pass `connector_machine_type` to networking; pass worker vars and pubsub subscription to monitoring |
| 18 | `terraform/environments/prod/main.tf` | modify | Pass `connector_machine_type = "e2-standard-4"` to networking; pass worker vars and pubsub subscription to monitoring |
| 19 | `.github/workflows/terraform.yml` | modify | Remove `continue-on-error: true`; replace verbatim `steps.plan.outputs.stdout` with filtered file content |
| 20 | `terraform/modules/kms/main.tf` | create | KMS key ring + crypto key resources, gated by `enable_cmek` |
| 21 | `terraform/modules/kms/variables.tf` | create | Variables: `project_id`, `region`, `environment`, `enable_cmek` (default false) |
| 22 | `terraform/modules/kms/outputs.tf` | create | Outputs: `key_ring_name`, `crypto_key_id` (empty strings when disabled) |
| 23 | `terraform/modules/kms/versions.tf` | create | Provider pin `~> 6.14.1` |

**Summary**: 4 files created, 19 files modified, 0 files deleted

---

## Interfaces and Contracts

### 1. Audit Logging — `terraform/modules/iam/main.tf`

Add after the existing `google_project_iam_member.sa_roles` resource. Remove `"roles/secretmanager.secretAccessor"` from the `sa_project_roles` role list.

```hcl
# C3: Project-level audit logging for all GCP services.
# Captures ADMIN_READ, DATA_READ, and DATA_WRITE across every API.
resource "google_project_iam_audit_config" "all_services" {
  project = var.project_id
  service = "allServices"

  audit_log_config {
    log_type = "ADMIN_READ"
  }

  audit_log_config {
    log_type = "DATA_READ"
  }

  audit_log_config {
    log_type = "DATA_WRITE"
  }
}
```

Modified `sa_project_roles` local (remove `secretmanager.secretAccessor`):

```hcl
locals {
  # Only project-level roles that cannot be scoped to individual resources.
  # Resource-scoped IAM (spanner, storage, pubsub, run.invoker) is handled
  # by each respective module for least-privilege.
  #
  # H2: roles/secretmanager.secretAccessor is intentionally absent.
  # Per-secret IAM bindings must be added via google_secret_manager_secret_iam_member
  # co-located with each google_secret_manager_secret resource when secrets are defined.
  sa_project_roles = flatten([
    for sa_key, sa_email in {
      api    = google_service_account.cloud_run_api.email
      worker = google_service_account.cloud_run_worker.email
      } : [
      for role in [
        "roles/logging.logWriter",
        "roles/cloudtrace.agent",
        ] : {
        key   = "${sa_key}-${replace(role, "roles/", "")}"
        email = sa_email
        role  = role
      }
    ]
  ])
}
```

---

### 2. Firewall Rules — `terraform/modules/networking/main.tf`

Append after the existing `google_vpc_access_connector.connector` resource. The VPC network reference uses `google_compute_network.vpc.id` — same pattern as the existing subnetwork resource.

```hcl
# -----------------------------------------------------------------------------
# Firewall Rules (C2)
# -----------------------------------------------------------------------------

# Deny all ingress — lowest-priority default; all specific allows above override this.
resource "google_compute_firewall" "deny_all_ingress" {
  name      = "deny-all-ingress-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "INGRESS"
  priority  = 65534

  deny {
    protocol = "all"
  }

  source_ranges = ["0.0.0.0/0"]
}

# Deny all egress — lowest-priority default.
resource "google_compute_firewall" "deny_all_egress" {
  name      = "deny-all-egress-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "EGRESS"
  priority  = 65534

  deny {
    protocol = "all"
  }

  destination_ranges = ["0.0.0.0/0"]
}

# Allow intra-subnet traffic (VPC connector instances communicating).
resource "google_compute_firewall" "allow_internal" {
  name      = "allow-internal-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "INGRESS"
  priority  = 1000

  allow {
    protocol = "tcp"
  }

  allow {
    protocol = "udp"
  }

  allow {
    protocol = "icmp"
  }

  source_ranges = [google_compute_subnetwork.connector_subnet.ip_cidr_range]
}

# Allow ingress from GCP health check ranges so the VPC connector passes health checks.
resource "google_compute_firewall" "allow_health_checks" {
  name      = "allow-health-checks-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "INGRESS"
  priority  = 1000

  allow {
    protocol = "tcp"
  }

  # GCP health check source ranges (documented at cloud.google.com/load-balancing/docs/health-checks)
  source_ranges = ["35.191.0.0/16", "130.211.0.0/22"]
}

# Allow egress to restricted.googleapis.com so Cloud Run can reach Spanner, Pub/Sub, GCS, etc.
resource "google_compute_firewall" "allow_gcp_apis" {
  name      = "allow-gcp-apis-egress-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "EGRESS"
  priority  = 1000

  allow {
    protocol = "tcp"
    ports    = ["443"]
  }

  # restricted.googleapis.com CIDR (199.36.153.8/30)
  destination_ranges = ["199.36.153.8/30"]
}

# -----------------------------------------------------------------------------
# Cloud Armor Security Policy (M1 — placeholder; not yet attached to a load balancer)
# Attachment requires a Global External Application Load Balancer.
# Track as follow-up change: glb-cloud-armor
# -----------------------------------------------------------------------------

resource "google_compute_security_policy" "rate_limit" {
  name    = "appetite-engine-rate-limit-${var.environment}"
  project = var.project_id

  rule {
    action   = "rate_based_ban"
    priority = 1000

    match {
      versioned_expr = "SRC_IPS_V1"

      config {
        src_ip_ranges = ["*"]
      }
    }

    rate_limit_options {
      rate_limit_threshold {
        count        = 1000
        interval_sec = 60
      }

      ban_duration_sec = 300

      conform_action = "allow"
      exceed_action  = "deny(429)"

      enforce_on_key = "IP"
    }

    description = "Rate limit: 1000 req/min per IP; ban 5 min on exceed"
  }

  rule {
    action   = "allow"
    priority = 2147483647

    match {
      versioned_expr = "SRC_IPS_V1"

      config {
        src_ip_ranges = ["*"]
      }
    }

    description = "Default allow rule"
  }
}
```

New variable in `terraform/modules/networking/variables.tf`:

```hcl
variable "connector_machine_type" {
  type        = string
  default     = "e2-micro"
  description = "Machine type for the VPC Access Connector. Use e2-standard-4 for prod (200 Mbps vs 2 Gbps throughput)."
}
```

Change `machine_type = "e2-micro"` in the connector resource to:

```hcl
  machine_type  = var.connector_machine_type
```

---

### 3. IAM Scoping — `terraform/bootstrap/main.tf`

Append after `google_service_account_iam_member.wif_terraform`. Uses `for_each` on a `toset` — same pattern as `google_project_iam_member.sa_roles` in the iam module.

```hcl
# -----------------------------------------------------------------------------
# Terraform SA Project Roles (H3)
# Enumerated minimal set. No roles/editor — each role scoped to one service domain.
# -----------------------------------------------------------------------------

locals {
  terraform_sa_roles = toset([
    "roles/compute.admin",
    "roles/iam.serviceAccountAdmin",
    "roles/run.admin",
    "roles/spanner.admin",
    "roles/pubsub.admin",
    "roles/storage.admin",
    "roles/monitoring.admin",
    "roles/vpcaccess.admin",
    "roles/artifactregistry.admin",
    "roles/secretmanager.admin",
    "roles/iam.workloadIdentityPoolAdmin",
  ])
}

resource "google_project_iam_member" "terraform_roles" {
  for_each = local.terraform_sa_roles

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.terraform.email}"
}
```

---

### 4. CI/CD Security — `.github/workflows/terraform.yml`

Two changes in the `plan` job:

**a) Remove `continue-on-error: true` from the Plan step and add a Show step:**

```yaml
      - name: Terraform Plan
        id: plan
        run: terraform plan -no-color -out=tfplan

      - name: Show Plan Summary
        id: plan_summary
        if: github.event_name == 'pull_request'
        run: |
          terraform show -no-color tfplan \
            | grep -E '^\s*(#|~|\+|-)' \
            | head -100 > plan_summary.txt
          echo "summary<<EOF" >> $GITHUB_OUTPUT
          cat plan_summary.txt >> $GITHUB_OUTPUT
          echo "EOF" >> $GITHUB_OUTPUT
```

**b) Replace verbatim `steps.plan.outputs.stdout` in the Post Plan step:**

```yaml
      - name: Post Plan to PR
        if: github.event_name == 'pull_request'
        uses: actions/github-script@v7
        with:
          script: |
            const output = `#### Terraform Plan - \`${{ matrix.environment }}\`

            \`\`\`
            ${{ steps.plan_summary.outputs.summary }}
            \`\`\`

            *Triggered by @${{ github.actor }}*`;

            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: output
            });
```

The existing `Plan Status` step (checks `steps.plan.outcome == 'failure'`) is retained unchanged — it now functions correctly because `continue-on-error: true` is removed, so a plan failure propagates the `failure` outcome.

---

### 5. Storage Security

**a) `terraform/bootstrap/main.tf` — state bucket:**

Add `public_access_prevention = "enforced"` to `google_storage_bucket.terraform_state`:

```hcl
resource "google_storage_bucket" "terraform_state" {
  name     = "${var.project_id}-terraform-state"
  location = var.region
  project  = var.project_id

  force_destroy = false

  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      num_newer_versions = 5
    }
    action {
      type = "Delete"
    }
  }

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"   # M3

  labels = {
    purpose = "terraform-state"
  }
}
```

**b) `terraform/modules/storage/main.tf` — Artifact Registry IAM (H5):**

Append after `google_storage_bucket_iam_member.worker_objectuser`:

```hcl
# H5: Scope Artifact Registry access to the runtime SAs only.
# Both API and Worker SAs need to pull their own images at Cloud Run startup.
resource "google_artifact_registry_repository_iam_member" "api_reader" {
  repository = google_artifact_registry_repository.docker.name
  location   = var.region
  project    = var.project_id
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${var.api_sa_email}"
}

resource "google_artifact_registry_repository_iam_member" "worker_reader" {
  repository = google_artifact_registry_repository.docker.name
  location   = var.region
  project    = var.project_id
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${var.worker_sa_email}"
}
```

**c) `terraform/modules/storage/main.tf` — AR vulnerability scanning (L1):**

Add `vulnerability_scanning_config` block to `google_artifact_registry_repository.docker`:

```hcl
resource "google_artifact_registry_repository" "docker" {
  repository_id = "appetite-engine-${var.environment}"
  format        = "DOCKER"
  location      = var.region
  project       = var.project_id
  description   = "Docker repository for appetite-engine images"

  vulnerability_scanning_config {
    enablement_config = "INHERITED"
  }

  labels = {
    environment = var.environment
  }
}
```

Note: `storage/variables.tf` requires no changes — `api_sa_email` and `worker_sa_email` are already declared.

---

### 6. Monitoring Worker — `terraform/modules/monitoring/`

**a) New variables in `terraform/modules/monitoring/variables.tf`:**

```hcl
variable "worker_service_name" {
  type        = string
  default     = ""
  description = "Cloud Run Worker service name for alert filters. Leave empty to disable worker alerts."
}

variable "worker_service_url" {
  type        = string
  default     = ""
  description = "Cloud Run Worker service URL. Required when worker_service_name is set."
}

variable "pubsub_subscription_name" {
  type        = string
  default     = ""
  description = "Pub/Sub push subscription name for DLQ backlog alert. Leave empty to disable."
}
```

**b) New resources in `terraform/modules/monitoring/main.tf`:**

Append after the existing `google_monitoring_uptime_check_config.api_health` resource. All three new alert resources mirror the API alert pattern exactly, substituting `var.worker_service_name`.

```hcl
# -----------------------------------------------------------------------------
# Worker Alert Policies (M4)
# All resources are no-ops when var.worker_service_name == "".
# -----------------------------------------------------------------------------

resource "google_monitoring_alert_policy" "worker_latency" {
  count = var.enable_alerts && var.worker_service_name != "" ? 1 : 0

  display_name = "Cloud Run ${var.worker_service_name} - High Latency"
  project      = var.project_id
  combiner     = "OR"

  conditions {
    display_name = "P99 latency > 2s"

    condition_threshold {
      filter = <<-EOT
        resource.type = "cloud_run_revision"
        AND resource.labels.service_name = "${var.worker_service_name}"
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
    service  = var.worker_service_name
  }
}

resource "google_monitoring_alert_policy" "worker_error_rate" {
  count = var.enable_alerts && var.worker_service_name != "" ? 1 : 0

  display_name = "Cloud Run ${var.worker_service_name} - High Error Rate"
  project      = var.project_id
  combiner     = "OR"

  conditions {
    display_name = "5xx error count > 5 per minute"

    condition_threshold {
      filter = <<-EOT
        resource.type = "cloud_run_revision"
        AND resource.labels.service_name = "${var.worker_service_name}"
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
    service  = var.worker_service_name
  }
}

resource "google_monitoring_alert_policy" "worker_cpu_utilization" {
  count = var.enable_alerts && var.worker_service_name != "" ? 1 : 0

  display_name = "Cloud Run ${var.worker_service_name} - CPU Utilization > 80%"
  project      = var.project_id
  combiner     = "OR"

  conditions {
    display_name = "CPU > 80% for 5m"

    condition_threshold {
      filter = <<-EOT
        resource.type = "cloud_run_revision"
        AND resource.labels.service_name = "${var.worker_service_name}"
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
    service  = var.worker_service_name
  }
}

resource "google_monitoring_alert_policy" "dlq_backlog" {
  count = var.enable_alerts && var.pubsub_subscription_name != "" ? 1 : 0

  display_name = "Pub/Sub DLQ ${var.pubsub_subscription_name} - Messages Backlogged"
  project      = var.project_id
  combiner     = "OR"

  conditions {
    display_name = "DLQ undelivered message count > 0"

    condition_threshold {
      filter = <<-EOT
        resource.type = "pubsub_subscription"
        AND resource.labels.subscription_id = "${var.pubsub_subscription_name}"
        AND metric.type = "pubsub.googleapis.com/subscription/num_undelivered_messages"
      EOT

      comparison      = "COMPARISON_GT"
      threshold_value = 0
      duration        = "300s"

      aggregations {
        alignment_period     = "60s"
        per_series_aligner   = "ALIGN_MEAN"
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
    content   = "Dead-letter queue `$${resource.labels.subscription_id}` has undelivered messages. Investigate Worker processing failures."
    mime_type = "text/markdown"
  }

  user_labels = {
    severity = "critical"
    service  = "pubsub"
  }
}
```

**c) Environment wiring — `terraform/environments/dev/main.tf` monitoring module call:**

```hcl
module "monitoring" {
  source                   = "../../modules/monitoring"
  project_id               = var.project_id
  service_name             = module.cloud_run_api.service_name
  service_url              = module.cloud_run_api.service_url
  notification_email       = var.ops_email
  enable_alerts            = false
  worker_service_name      = module.cloud_run_worker.service_name
  worker_service_url       = module.cloud_run_worker.service_url
  pubsub_subscription_name = module.pubsub.dlq_subscription_name
}
```

**d) Environment wiring — `terraform/environments/prod/main.tf` monitoring module call:**

```hcl
module "monitoring" {
  source                   = "../../modules/monitoring"
  project_id               = var.project_id
  service_name             = module.cloud_run_api.service_name
  service_url              = module.cloud_run_api.service_url
  notification_email       = var.ops_email
  enable_alerts            = true
  worker_service_name      = module.cloud_run_worker.service_name
  worker_service_url       = module.cloud_run_worker.service_url
  pubsub_subscription_name = module.pubsub.dlq_subscription_name
}
```

Note: `module.pubsub.dlq_subscription_name` requires a new output in `terraform/modules/pubsub/outputs.tf`:

```hcl
output "dlq_subscription_name" {
  value       = google_pubsub_subscription.appetite_events_dlq_pull.name
  description = "DLQ pull subscription name for monitoring alert filter"
}
```

**e) Environment wiring — networking module call (both dev and prod):**

Dev (`terraform/environments/dev/main.tf`):
```hcl
module "networking" {
  source                 = "../../modules/networking"
  project_id             = var.project_id
  region                 = var.region
  environment            = local.environment
  connector_machine_type = "e2-micro"
}
```

Prod (`terraform/environments/prod/main.tf`):
```hcl
module "networking" {
  source                 = "../../modules/networking"
  project_id             = var.project_id
  region                 = var.region
  environment            = local.environment
  connector_machine_type = "e2-standard-4"
}
```

---

### 7. Provider Pinning — All 8 `versions.tf` files

Every `versions.tf` in bootstrap and the 7 modules changes from:

```hcl
version = "~> 6.0"
```

to:

```hcl
version = "~> 6.14"
```

Full content after change (bootstrap `versions.tf`, same shape for all modules):

```hcl
terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.14"
    }
  }
}
```

Module `versions.tf` files (no `required_version` block by convention):

```hcl
terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.14"
    }
  }
}
```

Files to update:
1. `terraform/bootstrap/versions.tf`
2. `terraform/modules/iam/versions.tf`
3. `terraform/modules/networking/versions.tf`
4. `terraform/modules/spanner/versions.tf`
5. `terraform/modules/storage/versions.tf`
6. `terraform/modules/cloud-run/versions.tf`
7. `terraform/modules/pubsub/versions.tf`
8. `terraform/modules/monitoring/versions.tf`
9. `terraform/modules/kms/versions.tf` (new)

After pinning, run `terraform init -upgrade` in `terraform/bootstrap/`, `terraform/environments/dev/`, and `terraform/environments/prod/` to regenerate `.terraform.lock.hcl` files. Commit the updated lockfiles.

---

### 8. KMS Module (H1 — CMEK opt-in scaffold)

New module at `terraform/modules/kms/`. Not wired into any environment in this change. `enable_cmek` defaults to `false` so the module has zero infrastructure impact until deliberately enabled.

**`terraform/modules/kms/variables.tf`:**

```hcl
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
```

**`terraform/modules/kms/main.tf`:**

```hcl
resource "google_kms_key_ring" "main" {
  count = var.enable_cmek ? 1 : 0

  name     = "appetite-engine-${var.environment}"
  location = var.region
  project  = var.project_id
}

resource "google_kms_crypto_key" "main" {
  count = var.enable_cmek ? 1 : 0

  name            = "appetite-engine-key-${var.environment}"
  key_ring        = google_kms_key_ring.main[0].id
  rotation_period = "7776000s" # 90 days

  lifecycle {
    prevent_destroy = true
  }
}
```

**`terraform/modules/kms/outputs.tf`:**

```hcl
output "key_ring_name" {
  value       = var.enable_cmek ? google_kms_key_ring.main[0].name : ""
  description = "KMS key ring name. Empty string when enable_cmek = false."
}

output "crypto_key_id" {
  value       = var.enable_cmek ? google_kms_crypto_key.main[0].id : ""
  description = "KMS crypto key self-link. Empty string when enable_cmek = false."
}
```

**`terraform/modules/kms/versions.tf`:**

```hcl
terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 6.14"
    }
  }
}
```

---

## Testing Strategy

This is an IaC-only change. No Go unit tests apply. Verification is via Terraform tooling.

| What to Test | Type | Command / Check | Pass Criteria |
|---|---|---|---|
| All modules parse correctly | Static | `terraform validate` in bootstrap, dev, prod | Zero errors |
| HCL formatting | Static | `terraform fmt -check -recursive terraform/` | Zero diffs |
| Plan shows only adds/updates in dev | Plan | `terraform plan` in environments/dev | No destroy on existing resources |
| Audit config present | Plan | Grep plan output | `google_project_iam_audit_config.all_services` in adds |
| Firewall rules present | Plan | Grep plan output | 5 `google_compute_firewall` resources + 1 `google_compute_security_policy` in adds |
| State bucket `public_access_prevention` | Plan | Grep plan output | `public_access_prevention = "enforced"` in bootstrap plan |
| `secretAccessor` removed | Plan | Grep plan output | `roles/secretmanager.secretAccessor` absent from `google_project_iam_member.sa_roles` |
| Terraform SA roles present | Plan | Grep bootstrap plan | 11 `google_project_iam_member.terraform_roles` in adds |
| AR IAM members present | Plan | Grep plan output | `google_artifact_registry_repository_iam_member.api_reader` and `worker_reader` in adds |
| Worker monitoring resources present | Plan | Grep plan output | `worker_latency`, `worker_error_rate`, `worker_cpu_utilization`, `dlq_backlog` alert policies in adds |
| All versions.tf pinned | Static | `grep -r "~> 6.0" terraform/` | Zero matches |
| CI plan step has no continue-on-error | Static | `grep "continue-on-error" .github/workflows/terraform.yml` | Zero matches |
| CI plan comment has no `steps.plan.outputs.stdout` | Static | `grep "steps.plan.outputs.stdout" .github/workflows/terraform.yml` | Zero matches |

---

## Implementation Notes

**Ordering constraints during apply:**

1. Apply `terraform/bootstrap/` first (manual step; separate root). This provisions the Terraform SA roles (H3) and state bucket `public_access_prevention` (M3) before the pipeline runs.
2. Merge the PR — CI triggers plan on dev and prod in parallel.
3. On push to main, apply dev then prod (existing pipeline order).

**Backward compatibility:**

- `monitoring` module: `worker_service_name`, `worker_service_url`, `pubsub_subscription_name` all default to `""`. Callers that do not pass these variables are unaffected. The new alert resources use `count = var.enable_alerts && var.worker_service_name != "" ? 1 : 0`, so they are no-ops for callers that omit them.
- `networking` module: `connector_machine_type` defaults to `"e2-micro"` — the current hard-coded value. Callers that omit it behave identically to before.

**Provider lockfile:**

After updating all `versions.tf` files from `~> 6.0` to `~> 6.14`, run `terraform init -upgrade` in each of the three roots (`bootstrap/`, `environments/dev/`, `environments/prod/`) to regenerate `.terraform.lock.hcl`. Commit all three lockfiles in the same PR.

**`pubsub/outputs.tf` gap:**

The `module.pubsub.dlq_subscription_name` reference in the environment wiring requires a new output from the pubsub module. This file is not listed in the proposal's affected areas but is a necessary consequential change — add it to the implementation scope. The output references `google_pubsub_subscription.appetite_events_dlq_pull.name`, which already exists in `modules/pubsub/main.tf`.
