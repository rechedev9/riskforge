# Explore: terraform-gcp-module

## Current State

No Terraform code exists in the repository. The project is a Go 1.24 insurtech application ("appetite-engine") with a hexagonal architecture:

- `cmd/api/` -- Cloud Run HTTP server (REST API for appetite matching)
- `cmd/worker/` -- Pub/Sub subscriber (async event processing)
- `internal/domain/` -- Domain models (Carrier, AppetiteRule, RiskClassification, MatchResult)
- `internal/service/` -- Business logic (matching engine, AI classification)
- `internal/adapter/` -- Driven ports (Spanner repository, Pub/Sub publisher, Claude client)
- `internal/handler/` -- HTTP handlers (driving port)

The CLAUDE.md explicitly references `terraform/` as the IaC directory. The `.gitignore` already has Terraform entries targeting `terraform/.terraform/`, `terraform/*.tfstate`, `terraform/*.tfstate.*`, `terraform/*.tfplan`, and `terraform/.terraform.lock.hcl`.

However, the reference doc (`docs/terraform-gcp.md`) prescribes an `infrastructure/` root with `modules/` and `environments/` subdirectories. This naming conflict needs resolution.

The Go module is `github.com/reche-agentero/appetite-engine`. No Go source files exist yet -- only `go.mod`.

## Relevant Files

| File | Relevance |
|---|---|
| `CLAUDE.md` | Defines `terraform/` as IaC directory; commit conventions; architecture overview |
| `.gitignore` | Already has Terraform ignore rules under `terraform/` prefix |
| `go.mod` | Module name: `github.com/reche-agentero/appetite-engine`; Go 1.24.1 |
| `docs/terraform-gcp.md` | Comprehensive HCL reference: provider, state, Cloud Run, Spanner, Pub/Sub, IAM, networking, monitoring, storage, module organization, CI/CD |
| `docs/cloud-run.md` | Cloud Run container contract, health probes, Pub/Sub push integration, Terraform resource examples |
| `docs/cloud-spanner.md` | Spanner schema design, instance config, Terraform resource/module examples |
| `docs/cloud-pubsub.md` | Pub/Sub topics, subscriptions, DLQ, push with OIDC, IAM for dead letter forwarding |
| `openspec/config.yaml` | Project config: Go 1.24, build/test/lint/format commands |

## Dependency Map

```
infrastructure/
  environments/dev/main.tf
  environments/prod/main.tf
    --> modules/iam         (service accounts, WIF, project-level bindings)
    --> modules/networking   (VPC, connector)
    --> modules/spanner      (instance, database, DDL, database-level IAM)
    --> modules/cloud-run    (api service, worker service, resource-level IAM)
    --> modules/pubsub       (topics, subscriptions, push config, DLQ, IAM)
    --> modules/storage      (document bucket, terraform state bucket)
    --> modules/monitoring   (alert policies, uptime checks, notification channels)

.github/workflows/terraform.yml
    --> environments/dev/    (plan on PR, apply on merge)
    --> environments/prod/   (plan on PR, apply on merge with approval gate)
    --> WIF provider         (OIDC auth, no stored credentials)
```

Inter-module data flow:
- `iam` outputs SA emails --> consumed by `cloud-run`, `pubsub`, `spanner`, `storage`
- `networking` outputs VPC connector ID --> consumed by `cloud-run`
- `spanner` outputs instance/database names --> consumed by `cloud-run` (env vars)
- `cloud-run` outputs service URIs --> consumed by `pubsub` (push endpoint), `monitoring` (uptime checks)
- `pubsub` outputs topic IDs --> consumed by `cloud-run` (env vars for publisher)

## Risk Assessment

| Risk | Severity | Likelihood | Mitigation |
|---|---|---|---|
| Directory naming conflict (`terraform/` in CLAUDE.md/.gitignore vs `infrastructure/` in reference doc) | Medium | Certain | Decide one convention before writing code; update CLAUDE.md and .gitignore to match |
| Spanner cost in dev (100 PU minimum = ~$65/mo) | Low | Certain | Use processing_units=100 for dev; document cost expectations |
| Terraform state bucket chicken-and-egg | Medium | Certain | Create a `bootstrap/` directory for manual one-time setup (state bucket + WIF) |
| DDL drift between Terraform and application migrations | Medium | Likely | Keep initial schema in Terraform DDL; plan for migration tooling later |
| No GCP project exists yet | Low | Possible | Terraform modules should be project-agnostic via variables; project creation is out of scope |
| Secret Manager secrets referenced but not created | Low | Likely | Create placeholder secret resources in Terraform; actual values set manually |
| Push subscription OIDC requires Cloud Run URL (circular dependency) | Medium | Certain | Use `depends_on` or split into two applies; reference doc shows the pattern with direct resource references which Terraform handles automatically |
| .gitignore patterns use `terraform/` prefix but module uses `infrastructure/` | Low | Certain | Update .gitignore when directory name is decided |

## Approach Comparison

### Approach A: Flat `terraform/` directory (match CLAUDE.md)

```
terraform/
  modules/
    cloud-run/
    spanner/
    pubsub/
    iam/
    networking/
    monitoring/
    storage/
  environments/
    dev/
    prod/
  bootstrap/
```

**Pros:**
- Matches existing CLAUDE.md and .gitignore entries
- Simpler root directory (fewer top-level dirs)
- Clear separation: Go code at root, IaC in `terraform/`

**Cons:**
- Deviates from `infrastructure/` naming in the reference doc
- `.github/workflows/` path references need `terraform/` prefix

### Approach B: `infrastructure/` directory (match reference doc)

```
infrastructure/
  modules/
    cloud-run/
    spanner/
    pubsub/
    iam/
    networking/
    monitoring/
    storage/
  environments/
    dev/
    prod/
  bootstrap/
```

**Pros:**
- Matches the reference doc patterns exactly (copy-paste friendly)
- Industry-standard naming

**Cons:**
- Requires updating CLAUDE.md Architecture section
- Requires updating .gitignore patterns
- Two renames needed before writing any HCL

### Approach C: Raw resources only (no modules)

Single-directory Terraform with no module abstraction.

**Pros:**
- Faster to write initially
- No module interface overhead

**Cons:**
- No environment isolation without copy-paste
- Violates reference doc best practices
- Harder to maintain as resources grow

### Approach D: Official Google Terraform modules

Use `terraform-google-modules/*` community modules instead of raw resources.

**Pros:**
- Less code to write
- Encapsulates best practices

**Cons:**
- Less control over resource configuration
- Version pinning adds maintenance burden
- Abstractions may not match project needs exactly
- Reference doc uses raw resources throughout (except brief Spanner module mention)
- Harder to understand/debug for portfolio piece

## Recommendation

**Approach A: Flat `terraform/` directory** with raw resources (not community modules).

Rationale:
1. **CLAUDE.md is the source of truth** for this project. It already declares `terraform/` as the IaC directory, and `.gitignore` already has rules for it. Changing these creates unnecessary churn.
2. **Raw resources over community modules** -- this is a portfolio piece. Using raw `google_*` resources demonstrates deeper understanding of GCP and makes the code self-documenting. The reference doc (`docs/terraform-gcp.md`) provides complete HCL patterns for every resource needed.
3. **Modular structure** (`modules/` + `environments/`) is essential for dev/prod isolation. The reference doc Section 10 provides a complete module organization pattern.
4. **Bootstrap directory** for one-time setup (state bucket, WIF pool) keeps the chicken-and-egg problem isolated.

Module breakdown (7 modules):
- `iam` -- 3 service accounts (cloud-run-api, pubsub-invoker, terraform), project-level IAM bindings, WIF pool/provider
- `networking` -- VPC, /28 subnet, VPC Access connector
- `spanner` -- instance (processing_units), database with DDL, database-level IAM
- `cloud-run` -- generic service module (used for both api and worker), resource-level IAM
- `pubsub` -- topics (appetite-events, appetite-events-dlq), subscriptions (push to worker, pull for monitoring), DLQ IAM
- `storage` -- document bucket with lifecycle rules, bucket IAM
- `monitoring` -- notification channels, alert policies (latency, error rate, CPU), uptime checks

Each module: `main.tf`, `variables.tf`, `outputs.tf`, `versions.tf`.
Each environment: `backend.tf`, `main.tf`, `variables.tf`, `outputs.tf`, `terraform.tfvars`.

Variables parameterized for dev/prod:
- `project_id`, `region`
- `spanner_processing_units` (100 dev / 300+ prod)
- `min_instances`, `max_instances` (0/5 dev / 2/20 prod)
- `deletion_protection` (false dev / true prod)
- `image_tag` (per-deploy)
- `ops_email` (notification target)

CI/CD: GitHub Actions workflow with WIF auth, plan-on-PR, apply-on-merge, environment approval gates.

## Clarification Required (BLOCKING)

1. **Directory name**: CLAUDE.md says `terraform/` but the reference doc uses `infrastructure/`. Recommendation is `terraform/` to match existing config. Confirm?

## Open Questions (DEFERRED)

1. **GCP project ID**: What project ID will be used for dev and prod? (Can use placeholder variables for now.)
2. **Region**: Default `us-central1`? Or a different primary region?
3. **GitHub org/repo**: Needed for WIF attribute_condition. Can use variables with placeholder values.
4. **Notification targets**: Email address for ops alerts? Slack webhook? PagerDuty? (Can provision email channel only for now.)
5. **Domain/custom URL**: Will Cloud Run use a custom domain, or just the default `.run.app` URL?
6. **Spanner DDL**: Should initial schema (Carriers, AppetiteRules, RiskClassifications, MatchResults) be defined in Terraform, or deferred to a separate migration tool?
7. **Artifact Registry**: Should Terraform provision the Docker repository, or is it managed separately?
8. **API ingress**: Public (`INGRESS_TRAFFIC_ALL`) for the API, internal-only for the worker? Or both internal behind a load balancer?
