---
summary: "Terraform layout, environments and CI workflow for the GCP stack"
read_when: [infrastructure changes, Terraform modules, environments, deployment workflow]
---

# Infrastructure

The Terraform is a template for a Cloud Run, Spanner and Pub/Sub stack. The `terraform.tfvars` files hold placeholders (`YOUR_PROJECT_ID`, `ops@example.com`) and the backend files hold `YOUR_STATE_BUCKET_NAME`.

## Layout

- `terraform/bootstrap`: separate root. State bucket (public access prevention enforced), Workload Identity pool and GitHub provider, a `terraform` service account with 11 explicit admin roles and no `roles/editor`. This is the only place that sets `required_version >= 1.5`.
- `terraform/modules`: eight modules, listed below. Each pins the Google provider to `~> 6.14`.
- `terraform/environments/dev` and `prod`: wire the modules together.

## Modules

| Module | Resources |
|--------|-----------|
| iam | 3 service accounts (api, worker, Pub/Sub invoker), `logging.logWriter` and `cloudtrace.agent` for api and worker, project audit config for all services |
| networking | VPC, /28 subnet, VPC access connector, 5 firewall rules (deny all ingress, deny all egress, allow internal, allow health checks, allow egress to `199.36.153.8/30`), Cloud Armor policy |
| spanner | Instance, database with `Carriers`, `AppetiteRules`, `Quotes` and the `QuotesByExpiry` index, `databaseUser` binding for the service accounts |
| cloud-run | Generic Cloud Run v2 service, used twice: API (public) and worker (internal ingress, invoked by Pub/Sub) |
| pubsub | Events topic, dead-letter topic, push subscription with OIDC token (10 delivery attempts), DLQ pull subscription, IAM |
| storage | Versioned GCS bucket with public access prevention, Artifact Registry Docker repo with vulnerability scanning, IAM |
| monitoring | Email channel, uptime check, alert policies for API and worker (latency, 5xx, CPU) and DLQ backlog, all gated by `enable_alerts` |
| kms | Key ring and key behind `enable_cmek` (default false). No environment instantiates it |

The Cloud Armor policy (1000 requests per minute per IP, 5 minute ban) is a placeholder. It is not attached to anything because that needs a global load balancer, which is not in the repo.

## Environments

| | Dev | Prod |
|---|---|---|
| Cloud Run instances | 0 to 5 | 2 to 20 |
| Spanner processing units | 100 | 300 |
| Alerts | disabled | enabled |
| Deletion protection | off | on |
| VPC connector machine type | e2-micro | e2-standard-4 |

## Gaps between the Terraform and the Go code

- The Cloud Run API service gets `SPANNER_INSTANCE`, `SPANNER_DATABASE`, `PUBSUB_TOPIC`, `GCP_PROJECT` and `ENVIRONMENT`. The Go code reads `SPANNER_PROJECT` and requires `API_KEYS`, and it ignores the other three, so the deployed container would not start as configured.
- The worker image (`worker:...`) has no source in this repo.
- Resource names still say `appetite-engine`, the earlier name of the project.

## Workflow

`.github/workflows/terraform.yml` runs on pushes to `main` that touch `terraform/**`. It authenticates through Workload Identity Federation (variables `WIF_PROVIDER` and `TERRAFORM_SA`), runs `fmt -check`, `validate` and `plan` for dev and prod, then applies dev and then prod with `-auto-approve`. The plan-comment steps only run on `pull_request` events, and the workflow has no such trigger, so they never run.
