# Delta Spec: Environments

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The environments layer (`terraform/environments/dev/` and `terraform/environments/prod/`) wires all 7 modules together with environment-specific values. Each environment has its own GCS backend prefix for state isolation, its own `terraform.tfvars`, and its own module instantiation. The cloud-run module is instantiated twice per environment (API + worker).

## ADDED Requirements

### REQ-ENV-001: GCS Remote Backend per Environment

Each environment directory **MUST** contain a `backend.tf` declaring a `terraform.backend "gcs"` block. Dev **MUST** use `prefix = "environments/dev"` and prod **MUST** use `prefix = "environments/prod"`. Both **MUST** reference the same state bucket (created by bootstrap).

#### Scenario: Dev backend uses correct prefix · `code-based` · `critical`

- **WHEN** `terraform/environments/dev/backend.tf` is read
- **THEN** it contains `backend "gcs"` with `prefix = "environments/dev"`

#### Scenario: Prod backend uses correct prefix · `code-based` · `critical`

- **WHEN** `terraform/environments/prod/backend.tf` is read
- **THEN** it contains `backend "gcs"` with `prefix = "environments/prod"`

#### Scenario: Dev and prod state is isolated · `code-based` · `critical`

- **GIVEN** both `backend.tf` files are read
- **WHEN** the `prefix` values are compared
- **THEN** they are different strings (`"environments/dev"` vs `"environments/prod"`)

---

### REQ-ENV-002: Module Instantiation

Each environment `main.tf` **MUST** instantiate all 7 modules: `iam`, `networking`, `spanner`, `cloud-run` (twice: API + worker), `pubsub`, `storage`, `monitoring`. Module sources **MUST** use relative paths (`../../modules/{name}`).

#### Scenario: Dev main.tf instantiates all modules · `code-based` · `critical`

- **WHEN** `terraform/environments/dev/main.tf` is read
- **THEN** it contains `module "iam"`, `module "networking"`, `module "spanner"`, `module "pubsub"`, `module "storage"`, `module "monitoring"`, and two cloud-run module instances (for API and worker)

#### Scenario: Module sources use relative paths · `code-based` · `critical`

- **WHEN** all `module` blocks in `terraform/environments/dev/main.tf` are inspected
- **THEN** every `source` argument matches the pattern `"../../modules/{module_name}"`

---

### REQ-ENV-003: Inter-Module Wiring

The environments **MUST** wire module outputs to downstream module inputs correctly:
- `module.iam` SA email outputs -> `module.cloud_run_*`, `module.pubsub`, `module.spanner`, `module.storage`
- `module.networking.vpc_connector_id` -> `module.cloud_run_*`
- `module.spanner` instance/database names -> `module.cloud_run_api` and `module.cloud_run_worker` env vars
- `module.cloud_run_worker.service_url` -> `module.pubsub` push endpoint
- `module.cloud_run_api.service_url` -> `module.monitoring` uptime check

#### Scenario: Networking VPC connector wired to Cloud Run · `code-based` · `critical`

- **WHEN** the cloud-run module calls in `terraform/environments/dev/main.tf` are inspected
- **THEN** `vpc_connector_id = module.networking.vpc_connector_id` is set

#### Scenario: IAM SA emails wired to Cloud Run · `code-based` · `critical`

- **WHEN** the API cloud-run module call is inspected
- **THEN** `service_account_email = module.iam.api_sa_email`

#### Scenario: Worker service URL wired to Pub/Sub push endpoint · `code-based` · `critical`

- **WHEN** the pubsub module call is inspected
- **THEN** `push_endpoint` references the worker cloud-run module's `service_url` output

---

### REQ-ENV-004: Dev Environment Parameterization

The dev environment **MUST** set: `spanner_processing_units = 100`, `min_instances = 0` (scale-to-zero), `max_instances = 5`, `deletion_protection = false`.

#### Scenario: Dev uses 100 processing units · `code-based` · `critical`

- **WHEN** `terraform/environments/dev/terraform.tfvars` or the dev `main.tf` module calls are read
- **THEN** Spanner processing units are set to `100`

#### Scenario: Dev allows scale-to-zero · `code-based` · `critical`

- **WHEN** the dev cloud-run module calls are inspected
- **THEN** `min_instances = 0`

#### Scenario: Dev has deletion protection disabled · `code-based` · `critical`

- **WHEN** the dev spanner module call is inspected
- **THEN** `deletion_protection = false`

---

### REQ-ENV-005: Prod Environment Parameterization

The prod environment **MUST** set: `spanner_processing_units = 300`, `min_instances = 2`, `max_instances = 20`, `deletion_protection = true`.

#### Scenario: Prod uses 300 processing units · `code-based` · `critical`

- **WHEN** `terraform/environments/prod/terraform.tfvars` or the prod `main.tf` module calls are read
- **THEN** Spanner processing units are set to `300`

#### Scenario: Prod maintains warm instances · `code-based` · `critical`

- **WHEN** the prod cloud-run module calls are inspected
- **THEN** `min_instances = 2`

#### Scenario: Prod has deletion protection enabled · `code-based` · `critical`

- **WHEN** the prod spanner module call is inspected
- **THEN** `deletion_protection = true`

---

### REQ-ENV-006: API vs Worker Ingress Differentiation

The API cloud-run module instance **MUST** use `ingress = "INGRESS_TRAFFIC_ALL"`. The worker cloud-run module instance **MUST** use `ingress = "INGRESS_TRAFFIC_INTERNAL_ONLY"`.

#### Scenario: API has public ingress · `code-based` · `critical`

- **WHEN** the API cloud-run module call in `terraform/environments/dev/main.tf` is inspected
- **THEN** `ingress = "INGRESS_TRAFFIC_ALL"`

#### Scenario: Worker has internal-only ingress · `code-based` · `critical`

- **WHEN** the worker cloud-run module call in `terraform/environments/dev/main.tf` is inspected
- **THEN** `ingress = "INGRESS_TRAFFIC_INTERNAL_ONLY"`

---

### REQ-ENV-007: Environment Directory Structure

Each environment directory **MUST** contain `backend.tf`, `main.tf`, `variables.tf`, `outputs.tf`, and `terraform.tfvars`.

#### Scenario: Dev directory files present · `code-based` · `critical`

- **WHEN** the contents of `terraform/environments/dev/` are listed
- **THEN** `backend.tf`, `main.tf`, `variables.tf`, `outputs.tf`, and `terraform.tfvars` exist

#### Scenario: Prod directory files present · `code-based` · `critical`

- **WHEN** the contents of `terraform/environments/prod/` are listed
- **THEN** `backend.tf`, `main.tf`, `variables.tf`, `outputs.tf`, and `terraform.tfvars` exist

---

### REQ-ENV-008: Environment-Level Outputs

Each environment **MUST** output key infrastructure references: Cloud Run API URL, Cloud Run worker URL, Spanner instance name, Spanner database name.

#### Scenario: Dev outputs expose service URLs · `code-based` · `critical`

- **WHEN** `terraform/environments/dev/outputs.tf` is read
- **THEN** it exposes outputs referencing `module.cloud_run_api.service_url` and `module.cloud_run_worker.service_url` (or equivalent module names)

---

### REQ-ENV-009: Terraform Validation

Both environments **MUST** pass `terraform validate` after `terraform init -backend=false`. No syntax errors, missing variables, or broken module references **MUST** exist.

#### Scenario: Dev validates successfully · `code-based` · `critical`

- **WHEN** `terraform init -backend=false && terraform validate` is run in `terraform/environments/dev/`
- **THEN** the exit code is `0` and output contains `"Success"`

#### Scenario: Prod validates successfully · `code-based` · `critical`

- **WHEN** `terraform init -backend=false && terraform validate` is run in `terraform/environments/prod/`
- **THEN** the exit code is `0` and output contains `"Success"`

---

### REQ-ENV-010: No Hardcoded Values in Environments

Environment `.tf` files **MUST NOT** contain hardcoded GCP project IDs, regions, or credentials. All values **MUST** flow through `var.*` declarations with defaults or `terraform.tfvars` overrides.

#### Scenario: No hardcoded project ID in dev · `code-based` · `critical`

- **WHEN** all `.tf` files under `terraform/environments/dev/` are searched for literal GCP project ID patterns
- **THEN** no hardcoded project IDs are found; all references use `var.project_id`

---

## MODIFIED Requirements

(none)

## REMOVED Requirements

(none)

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-ENV-001    | ADDED | MUST     | 3         |
| REQ-ENV-002    | ADDED | MUST     | 2         |
| REQ-ENV-003    | ADDED | MUST     | 3         |
| REQ-ENV-004    | ADDED | MUST     | 3         |
| REQ-ENV-005    | ADDED | MUST     | 3         |
| REQ-ENV-006    | ADDED | MUST     | 2         |
| REQ-ENV-007    | ADDED | MUST     | 2         |
| REQ-ENV-008    | ADDED | MUST     | 1         |
| REQ-ENV-009    | ADDED | MUST     | 2         |
| REQ-ENV-010    | ADDED | MUST     | 1         |

**Total Requirements**: 10
**Total Scenarios**: 22

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-ENV-001 > Dev backend uses correct prefix | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-001 > Prod backend uses correct prefix | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-001 > Dev and prod state is isolated | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-002 > Dev main.tf instantiates all modules | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-002 > Module sources use relative paths | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-003 > Networking VPC connector wired to Cloud Run | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-003 > IAM SA emails wired to Cloud Run | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-003 > Worker service URL wired to Pub/Sub push endpoint | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-004 > Dev uses 100 processing units | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-004 > Dev allows scale-to-zero | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-004 > Dev has deletion protection disabled | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-005 > Prod uses 300 processing units | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-005 > Prod maintains warm instances | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-005 > Prod has deletion protection enabled | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-006 > API has public ingress | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-006 > Worker has internal-only ingress | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-007 > Dev directory files present | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-007 > Prod directory files present | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-008 > Dev outputs expose service URLs | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-009 > Dev validates successfully | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-009 > Prod validates successfully | code-based | critical | pass^3 = 1.00 |
| REQ-ENV-010 > No hardcoded project ID in dev | code-based | critical | pass^3 = 1.00 |
