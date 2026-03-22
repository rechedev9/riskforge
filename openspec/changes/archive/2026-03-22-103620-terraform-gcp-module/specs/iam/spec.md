# Delta Spec: IAM

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The IAM module creates dedicated service accounts for each workload (API, worker, Pub/Sub invoker) and applies least-privilege project-level IAM bindings. It uses additive `google_project_iam_member` resources to avoid clobbering bindings managed outside Terraform.

## ADDED Requirements

### REQ-IAM-001: Service Account Creation

The IAM module **MUST** create three `google_service_account` resources: one for the Cloud Run API service (`cloud-run-api`), one for the Cloud Run worker service (`cloud-run-worker`), and one for the Pub/Sub push invoker (`pubsub-invoker`). Each service account **MUST** include a `display_name` and `description`. The `account_id` **SHOULD** incorporate the environment name to prevent cross-environment collisions.

#### Scenario: Three service accounts declared · `code-based` · `critical`

- **WHEN** `terraform/modules/iam/main.tf` is read
- **THEN** three `google_service_account` resources exist with `account_id` values containing `"cloud-run-api"`, `"cloud-run-worker"`, and `"pubsub-invoker"`

#### Scenario: Service accounts scoped to project variable · `code-based` · `critical`

- **WHEN** `terraform/modules/iam/main.tf` is read
- **THEN** every `google_service_account` resource sets `project = var.project_id`

---

### REQ-IAM-002: Least-Privilege IAM Bindings

The IAM module **MUST** use `google_project_iam_member` (additive) for project-level bindings. The module **MUST NOT** use `google_project_iam_binding` or `google_project_iam_policy` to avoid clobbering external bindings.

The API SA **MUST** receive: `roles/spanner.databaseUser`, `roles/secretmanager.secretAccessor`, `roles/storage.objectUser`, `roles/logging.logWriter`, `roles/cloudtrace.agent`.

The worker SA **MUST** receive: `roles/spanner.databaseUser`, `roles/secretmanager.secretAccessor`, `roles/logging.logWriter`, `roles/cloudtrace.agent`, `roles/pubsub.subscriber`.

The Pub/Sub invoker SA **MUST** receive: `roles/run.invoker`.

#### Scenario: Only additive IAM member resources used · `code-based` · `critical`

- **WHEN** `terraform/modules/iam/main.tf` is searched for `google_project_iam_binding` or `google_project_iam_policy`
- **THEN** no matches are found

#### Scenario: API SA has spanner databaseUser role · `code-based` · `critical`

- **WHEN** `terraform/modules/iam/main.tf` is read
- **THEN** a `google_project_iam_member` resource grants `roles/spanner.databaseUser` to the API service account

#### Scenario: Pub/Sub invoker SA has only run.invoker role · `code-based` · `critical`

- **WHEN** all `google_project_iam_member` resources referencing the `pubsub-invoker` SA are collected
- **THEN** the only role assigned is `roles/run.invoker`

---

### REQ-IAM-003: IAM Module Outputs

The IAM module **MUST** output `api_sa_email`, `worker_sa_email`, and `pubsub_invoker_sa_email` for consumption by other modules (cloud-run, pubsub, spanner, storage).

#### Scenario: IAM outputs declared · `code-based` · `critical`

- **WHEN** `terraform/modules/iam/outputs.tf` is read
- **THEN** it declares `output "api_sa_email"`, `output "worker_sa_email"`, and `output "pubsub_invoker_sa_email"`

---

### REQ-IAM-004: IAM Module Structure

The IAM module **MUST** contain `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf`. The `versions.tf` **MUST** declare `required_providers` with `hashicorp/google ~> 6.0`.

#### Scenario: IAM module files present · `code-based` · `critical`

- **WHEN** the contents of `terraform/modules/iam/` are listed
- **THEN** the files `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf` exist

---

### REQ-IAM-005: No Hardcoded Values

The IAM module **MUST NOT** contain hardcoded project IDs, regions, or SA email addresses. All values **MUST** be parameterized via `var.project_id` and `var.environment`.

#### Scenario: No hardcoded project ID in IAM module · `code-based` · `critical`

- **WHEN** all `.tf` files under `terraform/modules/iam/` are searched for literal GCP project ID patterns
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
| REQ-IAM-001    | ADDED | MUST     | 2         |
| REQ-IAM-002    | ADDED | MUST     | 3         |
| REQ-IAM-003    | ADDED | MUST     | 1         |
| REQ-IAM-004    | ADDED | MUST     | 1         |
| REQ-IAM-005    | ADDED | MUST     | 1         |

**Total Requirements**: 5
**Total Scenarios**: 8

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-IAM-001 > Three service accounts declared | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-001 > Service accounts scoped to project variable | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-002 > Only additive IAM member resources used | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-002 > API SA has spanner databaseUser role | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-002 > Pub/Sub invoker SA has only run.invoker role | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-003 > IAM outputs declared | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-004 > IAM module files present | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-005 > No hardcoded project ID in IAM module | code-based | critical | pass^3 = 1.00 |
