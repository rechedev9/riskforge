<p align="center">
  <img src="assets/banner.png" alt="RISKFORGE — GCP Infrastructure for Insurtech" width="720">
</p>

<h1 align="center">riskforge — GCP Infrastructure for Insurtech</h1>

<p align="center">
  <strong>Production-grade Terraform modules for Cloud Run + Spanner + Pub/Sub.<br>Built to demonstrate GCP, Go, and IaC competency for insurance technology platforms.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Terraform-1.5+-7B42BC?style=for-the-badge&logo=terraform" alt="Terraform 1.5+">
  <img src="https://img.shields.io/badge/GCP-Cloud_Run_|_Spanner_|_Pub/Sub-4285F4?style=for-the-badge&logo=googlecloud" alt="GCP">
  <img src="https://img.shields.io/badge/Modules-7-7C3AED?style=for-the-badge" alt="Modules: 7">
  <img src="https://img.shields.io/badge/Go-1.24-00ADD8?style=for-the-badge&logo=go" alt="Go 1.24">
  <img src="https://img.shields.io/badge/License-MIT-blue?style=for-the-badge" alt="MIT License">
</p>

<p align="center">
  <a href="#architecture">Architecture</a> ·
  <a href="#modules">Modules</a> ·
  <a href="#quick-start">Quick Start</a> ·
  <a href="#environments">Environments</a> ·
  <a href="#cicd">CI/CD</a> ·
  <a href="docs/terraform-gcp.md">Reference</a>
</p>

---

Infrastructure-as-Code for an insurance carrier appetite matching engine. Seven Terraform modules provision a complete GCP stack — from IAM and networking through compute, storage, messaging, and observability. Two environment configurations (dev/prod) demonstrate production-grade separation with parameterized scaling, deletion protection, and alert gating.

```
What this provisions:

Cloud Run (API + Worker)  ──→  Spanner (Carriers, AppetiteRules)
        │                              │
        └── Pub/Sub (events) ──────────┘
        │
        └── Monitoring (latency, errors, CPU, uptime)
        │
VPC + Connector ── IAM (3 SAs, least-privilege) ── Storage (docs + Artifact Registry)
```

---

## Architecture

```
terraform/
├── bootstrap/                    # One-time: state bucket, WIF, SA
├── modules/
│   ├── iam/                      # 3 service accounts, for_each IAM bindings
│   ├── networking/               # VPC, /28 subnet, VPC Access connector
│   ├── spanner/                  # Instance, database, DDL, database-level IAM
│   ├── cloud-run/                # Generic module (instantiated 2x: API + worker)
│   ├── pubsub/                   # Topic, DLQ, push subscription, OIDC auth
│   ├── storage/                  # Document bucket, Artifact Registry
│   └── monitoring/               # 3 alert policies, uptime check, email channel
├── environments/
│   ├── dev/                      # Scale-to-zero, 100 PU, alerts disabled
│   └── prod/                     # Min 2 instances, 300 PU, deletion protection
└── .github/workflows/
    └── terraform.yml             # WIF auth, plan-on-PR, sequential apply
```

### Inter-Module Dependency Graph

```
                    ┌─── module.iam ───┐
                    │   (3 SAs, IAM)    │
                    └─────┬─────┬──────┘
                          │     │
              ┌───────────┤     ├───────────┐
              │           │     │           │
     module.spanner  module.storage  module.cloud_run_api
     (instance, DB)  (bucket, AR)   (public, allUsers)
              │                            │
              │                     module.cloud_run_worker
              │                     (internal, SA-only)
              │                            │
              │                     module.pubsub
              │                     (push + OIDC + DLQ)
              │                            │
              │                     module.monitoring
              │                     (alerts, uptime)
              │
     module.networking
     (VPC, connector)
```

---

## Modules

| Module | Resources | Variables | Outputs |
|--------|-----------|-----------|---------|
| **iam** | 3 SAs, 6 IAM bindings (for_each) | project_id, environment | api_sa_email, worker_sa_email, pubsub_invoker_sa_email |
| **networking** | VPC, /28 subnet, VPC connector, API enable | project_id, region, environment | vpc_connector_id, vpc_id |
| **spanner** | Instance, database (DDL), database IAM | project_id, region, environment, processing_units, deletion_protection, sa_emails | instance_name, database_name |
| **cloud-run** | Cloud Run v2 service, conditional IAM | service_name, image, min/max_instances, env_vars, secret_env_vars, ingress, allow_unauthenticated, ... | service_url, service_name, service_id |
| **pubsub** | Topic, DLQ topic, push subscription, DLQ pull, 4 IAM members | project_id, environment, push_endpoint, invoker_sa_email, worker_sa_email, api_sa_email | topic_id, topic_name, subscription_id |
| **storage** | GCS bucket, Artifact Registry, 2 bucket IAM | project_id, region, environment, api_sa_email, worker_sa_email | bucket_name, bucket_url, registry_url |
| **monitoring** | 3 alert policies, uptime check, email channel | project_id, service_name, service_url, notification_email, enable_alerts | notification_channel_id, alert_policy_ids |

### Key Design Decisions

| Decision | Choice | Why |
|----------|--------|-----|
| Raw `google_*` resources | No community modules | Portfolio piece — demonstrates deep GCP knowledge |
| Additive IAM (`iam_member`) | Not `iam_binding` / `iam_policy` | Won't clobber bindings managed outside Terraform |
| Resource-scoped IAM | Not project-level | Least-privilege: Spanner/storage/pubsub IAM at resource level |
| Generic Cloud Run module | Instantiated 2x per env | API + worker share 90% config; DRY without premature abstraction |
| `for_each` IAM | Not 11 individual resources | Single map-driven resource block; easy to add/remove roles |
| `enable_alerts` toggle | Monitoring gated per env | Dev: no alerts (prevents noise + scale-to-zero interference) |
| Sequential CI/CD | dev deploys before prod | Failed dev apply blocks prod; environment protection gates |

---

## Quick Start

### Prerequisites

- [Terraform 1.5+](https://developer.hashicorp.com/terraform/install)
- [Google Cloud SDK](https://cloud.google.com/sdk/docs/install)
- A GCP project with billing enabled

### 1. Bootstrap (one-time)

```bash
cd terraform/bootstrap
cp terraform.tfvars.example terraform.tfvars
# Edit: project_id, github_org, github_repo

terraform init
terraform apply
# Outputs: state_bucket_name, wif_provider_name, terraform_sa_email
```

### 2. Configure environment

```bash
cd terraform/environments/dev
# Update backend.tf with the state_bucket_name from bootstrap
# Update terraform.tfvars with project_id, image_tag, ops_email

terraform init
terraform plan
terraform apply
```

### 3. Verify

```bash
# Format check (all modules)
terraform fmt -check -recursive terraform/

# Validate (per environment)
cd terraform/environments/dev && terraform validate
cd terraform/environments/prod && terraform validate
```

---

## Environments

| Parameter | Dev | Prod |
|-----------|-----|------|
| `min_instances` | 0 (scale-to-zero) | 2 |
| `max_instances` | 5 | 20 |
| `spanner_processing_units` | 100 | 300 |
| `deletion_protection` | false | true |
| `resource_limits` | 1 vCPU, 512Mi | 2 vCPU, 1Gi |
| `enable_alerts` | false | true |
| API ingress | Public (`INGRESS_TRAFFIC_ALL`) | Public |
| Worker ingress | Internal only | Internal only |

All environment-specific values flow through `locals.environment` — a single source of truth per environment root module, referenced by every module call.

---

## CI/CD

GitHub Actions workflow (`.github/workflows/terraform.yml`):

```
PR opened ──→ Plan (dev) ──→ Plan (prod) ──→ Comment on PR
                                                    ↓
Main merged ──→ Apply (dev) ──→ [success] ──→ Apply (prod)
                                                    ↑
                                          Environment gate
```

- **Auth**: Workload Identity Federation (zero stored credentials)
- **Plan**: Parallel for dev + prod on every PR
- **Apply**: Sequential — dev must succeed before prod starts
- **Protection**: GitHub environment rules gate prod deployment
- **Format**: `terraform fmt -check -recursive` runs in plan job

---

## Spanner Schema

The Spanner module includes initial DDL for the insurance domain:

```sql
CREATE TABLE Carriers (
  CarrierId     STRING(36) NOT NULL DEFAULT (GENERATE_UUID()),
  Name          STRING(255) NOT NULL,
  Code          STRING(50) NOT NULL,
  IsActive      BOOL NOT NULL DEFAULT (true),
  CreatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
  UpdatedAt     TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (CarrierId)

CREATE TABLE AppetiteRules (
  RuleId              STRING(36) NOT NULL DEFAULT (GENERATE_UUID()),
  CarrierId           STRING(36) NOT NULL,
  State               STRING(2) NOT NULL,
  LineOfBusiness      STRING(100) NOT NULL,
  ClassCode           STRING(50),
  MinPremium          FLOAT64,
  MaxPremium          FLOAT64,
  IsActive            BOOL NOT NULL DEFAULT (true),
  EligibilityCriteria JSON,
  CreatedAt           TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
) PRIMARY KEY (CarrierId, RuleId),
  INTERLEAVE IN PARENT Carriers ON DELETE CASCADE
```

Interleaved tables co-locate appetite rules with their parent carrier for efficient queries.

---

## Project Structure

```
.
├── CLAUDE.md                         # Agent protocol and conventions
├── README.md
├── go.mod                            # Go 1.24 module
├── docs/
│   ├── cloud-run.md                  # Cloud Run Go reference (1530 lines)
│   ├── cloud-spanner.md              # Spanner Go client reference
│   ├── cloud-pubsub.md               # Pub/Sub Go client reference
│   ├── terraform-gcp.md              # Terraform GCP reference (1991 lines)
│   ├── anthropic-go-sdk.md           # Claude API Go SDK reference
│   ├── opentelemetry-go.md           # OpenTelemetry Go on GCP
│   ├── handoff.md                    # Session handoff template
│   └── pickup.md                     # Session pickup template
├── terraform/
│   ├── bootstrap/                    # State bucket + WIF (4 files)
│   ├── modules/                      # 7 reusable modules (33 files)
│   └── environments/                 # dev + prod configs (10 files)
├── .github/workflows/
│   └── terraform.yml                 # Plan + apply pipeline
├── openspec/                         # SDD artifacts (archived)
└── scripts/
    ├── committer                     # Git commit helper
    └── docs-list                     # Docs compliance checker
```

---

## Docs

| Doc | Lines | Covers |
|-----|-------|--------|
| [Cloud Run](docs/cloud-run.md) | 1,530 | Container contract, Go patterns, Pub/Sub push, Eventarc, IAM, scaling |
| [Cloud Spanner](docs/cloud-spanner.md) | 1,185 | Schema design, transactions, change streams, testing, Terraform |
| [Cloud Pub/Sub](docs/cloud-pubsub.md) | 819 | Publish/subscribe, exactly-once, DLQ, retry, flow control |
| [Terraform GCP](docs/terraform-gcp.md) | 1,991 | Provider, state, Cloud Run/Spanner/Pub/Sub modules, IAM, CI/CD |
| [Anthropic Go SDK](docs/anthropic-go-sdk.md) | 1,020 | Messages, tools, structured output, PDF, streaming, batch |
| [OpenTelemetry Go](docs/opentelemetry-go.md) | 1,004 | Tracing, metrics, Spanner instrumentation, Cloud Run, slog |

All docs include YAML front-matter with `summary` and `read_when` fields for on-demand loading.

---

## Built With

- **[Terraform](https://www.terraform.io/)** — Infrastructure as Code
- **[Google Cloud Platform](https://cloud.google.com/)** — Cloud Run, Spanner, Pub/Sub, IAM, Monitoring
- **[Go](https://go.dev/)** — Application runtime (Cloud Run services)
- **[GitHub Actions](https://github.com/features/actions)** — CI/CD with Workload Identity Federation
- **[SDD](https://github.com/rechedev9/shenronSDD)** — Spec-Driven Development pipeline

## License

MIT
