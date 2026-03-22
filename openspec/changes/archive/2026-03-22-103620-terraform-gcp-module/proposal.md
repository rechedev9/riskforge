# Proposal: terraform-gcp-module

## Intent

Provision production-grade GCP infrastructure via Terraform for the appetite-engine application. The stack covers Cloud Run (API + worker), Spanner, Pub/Sub, IAM, networking, storage, and monitoring -- all using raw `google_*` resources (no community modules) to demonstrate deep GCP understanding as a portfolio piece.

## Scope

### In Scope

- 7 reusable Terraform modules under `terraform/modules/`: `iam`, `networking`, `spanner`, `cloud-run`, `pubsub`, `storage`, `monitoring`
- 2 environment configurations: `terraform/environments/dev/` and `terraform/environments/prod/`
- Bootstrap directory (`terraform/bootstrap/`) for state bucket and Workload Identity Federation pool
- Artifact Registry repository for Docker images
- Initial Spanner DDL (Carriers, AppetiteRules tables) embedded in Terraform
- GitHub Actions CI/CD workflow (`.github/workflows/terraform.yml`) with WIF auth, plan-on-PR, apply-on-merge
- Secret Manager placeholder resources for runtime secrets (CLAUDE_API_KEY)

### Out of Scope

- GCP project creation (assumed pre-existing, parameterized via `var.project_id`)
- Custom domain / SSL certificate provisioning (uses default `.run.app` URL)
- Application deployment (Terraform provisions infra; CI/CD for app is separate)
- Multi-region / disaster recovery configuration
- VPN / interconnect networking
- Database migration tooling beyond initial DDL
- Cost optimization automation (autoscaler policies beyond Cloud Run built-in)

## Approach

### Directory Structure

```
terraform/
  bootstrap/
    main.tf              # State bucket, WIF pool/provider
    variables.tf
    outputs.tf
    versions.tf
  modules/
    iam/
      main.tf            # Service accounts, project IAM bindings
      variables.tf
      outputs.tf
      versions.tf
    networking/
      main.tf            # VPC, subnet, VPC Access connector
      variables.tf
      outputs.tf
      versions.tf
    spanner/
      main.tf            # Instance, database with DDL, database IAM
      variables.tf
      outputs.tf
      versions.tf
    cloud-run/
      main.tf            # Generic service (reused for api + worker), resource IAM
      variables.tf
      outputs.tf
      versions.tf
    pubsub/
      main.tf            # Topics, subscriptions, push config, DLQ, IAM
      variables.tf
      outputs.tf
      versions.tf
    storage/
      main.tf            # Document bucket, lifecycle rules, bucket IAM
      variables.tf
      outputs.tf
      versions.tf
    monitoring/
      main.tf            # Alert policies, uptime checks
      channels.tf        # Notification channels
      variables.tf
      outputs.tf
      versions.tf
  environments/
    dev/
      backend.tf         # GCS backend, prefix "environments/dev"
      main.tf            # Module calls with dev values
      variables.tf
      outputs.tf
      terraform.tfvars   # Dev-specific values
    prod/
      backend.tf         # GCS backend, prefix "environments/prod"
      main.tf            # Module calls with prod values
      variables.tf
      outputs.tf
      terraform.tfvars   # Prod-specific values
.github/
  workflows/
    terraform.yml        # CI/CD pipeline
```

**Total: ~30 new files**

### Module Design

Each module follows the Terraform standard module structure (`main.tf`, `variables.tf`, `outputs.tf`, `versions.tf`) and uses only raw `google_*` provider resources.

**Inter-module data flow** (environments wire these via module outputs):
1. `iam` outputs SA emails -> consumed by `cloud-run`, `pubsub`, `spanner`, `storage`
2. `networking` outputs VPC connector ID -> consumed by `cloud-run`
3. `spanner` outputs instance/database names -> consumed by `cloud-run` (env vars)
4. `cloud-run` outputs service URIs -> consumed by `pubsub` (push endpoint), `monitoring` (uptime checks)
5. `pubsub` outputs topic IDs -> consumed by `cloud-run` (env vars for publisher)

**cloud-run module is generic**: instantiated twice per environment (once for `api`, once for `worker`) with different ingress, scaling, and env var configurations.

### Environment Parameterization

| Variable | Dev | Prod |
|---|---|---|
| `spanner_processing_units` | 100 | 300 |
| `min_instances` | 0 | 2 |
| `max_instances` | 5 | 20 |
| `deletion_protection` | false | true |
| `image_tag` | parameterized | parameterized |
| `ops_email` | parameterized | parameterized |

### Bootstrap Workflow

The `terraform/bootstrap/` directory handles the chicken-and-egg problem:
1. Run manually once with local state: `terraform init && terraform apply`
2. Creates: GCS state bucket, WIF identity pool, WIF GitHub provider, Terraform SA
3. After bootstrap, environments use the created bucket as their remote backend

### CI/CD Pipeline

GitHub Actions workflow at `.github/workflows/terraform.yml`:
- Triggers on PR and push to `main` for paths under `terraform/` and `.github/workflows/terraform.yml`
- Uses WIF auth (no stored credentials)
- Matrix strategy for dev/prod environments
- PR: `terraform plan` with PR comment output
- Merge to main: `terraform apply` with environment protection rules (prod requires approval)

### HCL Patterns

All HCL follows the patterns in `docs/terraform-gcp.md`:
- Provider: `hashicorp/google ~> 6.0` and `hashicorp/google-beta ~> 6.0`
- Terraform version: `>= 1.5`
- Cloud Run v2 API (`google_cloud_run_v2_service`)
- Spanner with `processing_units` (not `num_nodes`)
- Pub/Sub push subscriptions with OIDC auth
- VPC Access connector on `/28` subnet
- `google_monitoring_alert_policy` for latency, error rate, CPU alerts

## Key Decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | Root directory `terraform/` (not `infrastructure/`) | Matches CLAUDE.md architecture section and existing `.gitignore` patterns; avoids churn |
| 2 | Raw `google_*` resources (not community modules) | Portfolio piece; demonstrates deep GCP knowledge; `docs/terraform-gcp.md` provides all patterns |
| 3 | Generic `cloud-run` module instantiated 2x | API and worker share 90% of config; dynamic blocks handle differences (ingress, scaling, env vars) |
| 4 | Bootstrap directory with local state | Clean separation of chicken-and-egg resources; run once manually, then forget |
| 5 | Spanner DDL in Terraform | Include initial schema (Carriers, AppetiteRules); migration tooling deferred to later change |
| 6 | Artifact Registry in Terraform | Module `storage` provisions Docker repo alongside document bucket; keeps all infra in one place |
| 7 | API ingress public, worker internal | API serves external clients; worker receives Pub/Sub push (internal only via IAM invoker) |
| 8 | Email-only notification channel | Start simple; Slack/PagerDuty can be added later without breaking changes |
| 9 | Default region `us-central1` | Standard GCP region with broad service availability; parameterized for override |
| 10 | Monitoring module with separate `channels.tf` | Follows reference doc pattern; keeps alert policies and notification channels in distinct files |

## Affected Areas

| Area | Type | Description |
|---|---|---|
| `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/main.tf` | New | State bucket, WIF pool, WIF provider, Terraform SA |
| `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/variables.tf` | New | project_id, region, github_org, github_repo |
| `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/outputs.tf` | New | state_bucket_name, wif_provider_name, terraform_sa_email |
| `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/versions.tf` | New | Required providers block |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/main.tf` | New | 3 SAs (cloud-run-api, cloud-run-worker, pubsub-invoker), project IAM bindings |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/variables.tf` | New | project_id, environment |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/outputs.tf` | New | api_sa_email, worker_sa_email, pubsub_invoker_sa_email |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/versions.tf` | New | Required providers |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/main.tf` | New | VPC, /28 subnet, VPC Access connector |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/variables.tf` | New | project_id, region, environment |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/outputs.tf` | New | vpc_connector_id, vpc_id |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/versions.tf` | New | Required providers |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/main.tf` | New | Instance, database with DDL (Carriers, AppetiteRules), database IAM |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/variables.tf` | New | project_id, region, processing_units, sa_emails, deletion_protection |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/outputs.tf` | New | instance_name, database_name |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/versions.tf` | New | Required providers |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | New | Generic service, resource-level IAM (public for api, invoker for worker) |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/variables.tf` | New | service_name, image, scaling, env_vars, secret_env_vars, ingress, vpc_connector_id |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/outputs.tf` | New | service_url, service_name, service_id |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/versions.tf` | New | Required providers |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/main.tf` | New | Topics (appetite-events, DLQ), subscriptions (push to worker, pull for DLQ), IAM |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/variables.tf` | New | project_id, push_endpoint, invoker_sa_email, subscriber_sa_email |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/outputs.tf` | New | topic_id, topic_name, subscription_id |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/versions.tf` | New | Required providers |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/main.tf` | New | Document bucket, Artifact Registry repo, lifecycle rules, bucket IAM |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/variables.tf` | New | project_id, region, sa_emails, environment |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/outputs.tf` | New | bucket_name, bucket_url, registry_url |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/versions.tf` | New | Required providers |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/main.tf` | New | Alert policies (latency, error rate, CPU), uptime checks |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/channels.tf` | New | Email notification channel |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/variables.tf` | New | project_id, service_name, service_url, notification_email |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/outputs.tf` | New | notification_channel_id, alert_policy_ids |
| `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/versions.tf` | New | Required providers |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/backend.tf` | New | GCS backend with "environments/dev" prefix |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/main.tf` | New | Module calls with dev values (scale-to-zero, 100 PU, no deletion protection) |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/variables.tf` | New | Shared variable declarations |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/outputs.tf` | New | Environment-level outputs (URLs, instance names) |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/terraform.tfvars` | New | Dev-specific values |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/backend.tf` | New | GCS backend with "environments/prod" prefix |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/main.tf` | New | Module calls with prod values (min 2 instances, 300 PU, deletion protection) |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/variables.tf` | New | Shared variable declarations |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/outputs.tf` | New | Environment-level outputs |
| `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/terraform.tfvars` | New | Prod-specific values |
| `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | New | CI/CD: WIF auth, plan-on-PR, apply-on-merge, matrix dev/prod |

## Risks

| # | Risk | Severity | Likelihood | Mitigation |
|---|---|---|---|---|
| 1 | Spanner cost in dev (~$65/mo at 100 PU minimum) | Low | Certain | Document cost; use 100 PU minimum; `deletion_protection=false` in dev for easy teardown |
| 2 | Push subscription circular dependency (Pub/Sub needs Cloud Run URL) | Medium | Certain | Terraform handles this automatically via resource references; `cloud-run` module outputs `service_url` consumed by `pubsub` module in the same apply |
| 3 | DDL drift between Terraform and future app migrations | Medium | Likely | Initial schema in Terraform; add `lifecycle { ignore_changes = [ddl] }` after first apply when migration tooling is adopted |
| 4 | Bootstrap requires manual execution before environments work | Low | Certain | Document the one-time bootstrap process in a README within `terraform/bootstrap/`; bootstrap outputs feed into environment tfvars |
| 5 | No GCP project or billing account exists yet | Low | Possible | All resources parameterized via `var.project_id`; `terraform validate` works without a project; `terraform plan` requires a project |
| 6 | Secret Manager secrets referenced but not populated | Low | Certain | Create placeholder `google_secret_manager_secret` resources; actual values set manually post-apply |
| 7 | WIF attribute_condition requires exact GitHub org/repo match | Low | Likely | Parameterize `github_org` and `github_repo` variables; validate format in variable definition |

## Rollback Plan

This change creates only new files under `terraform/` and `.github/workflows/terraform.yml`. No existing files are modified.

**Rollback**: Delete the `terraform/` directory and `.github/workflows/terraform.yml`. No infrastructure exists until `terraform apply` is explicitly run, so file deletion is sufficient. If bootstrap has been applied, run `terraform destroy` in `terraform/bootstrap/` first.

## Dependencies

| Dependency | Type | Status |
|---|---|---|
| `docs/terraform-gcp.md` | Reference | Available -- provides all HCL patterns |
| `docs/cloud-run.md` | Reference | Available -- container contract, health probes |
| `docs/cloud-spanner.md` | Reference | Available -- schema design patterns |
| `docs/cloud-pubsub.md` | Reference | Available -- Pub/Sub topic/subscription patterns |
| GCP project with billing | Runtime | Required for `terraform plan/apply`; not needed for `validate` |
| Terraform CLI >= 1.5 | Tooling | Required locally and in CI |
| `hashicorp/google` provider ~> 6.0 | Terraform | Downloaded on `terraform init` |
| Go application container images | Runtime | Required for Cloud Run deploy; placeholder image used in Terraform |

## Success Criteria

1. `terraform validate` passes in both `terraform/environments/dev/` and `terraform/environments/prod/` (after `terraform init` with backend config disabled: `terraform init -backend=false`)
2. `terraform plan` produces clean output (no errors) when run against a real GCP project
3. All 7 modules contain `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf`
4. Both environments use remote GCS state backend (configured in `backend.tf`)
5. Bootstrap directory is self-contained and can create the state bucket + WIF resources independently
6. CI/CD workflow validates, plans on PR, and applies on merge with environment protection
7. Inter-module dependencies are wired correctly: no dangling references between module outputs and inputs
8. No hardcoded project IDs, regions, or credentials in any `.tf` file -- all parameterized via variables
