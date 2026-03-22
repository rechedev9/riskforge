# Delta Spec: Bootstrap

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The bootstrap module solves the chicken-and-egg problem: it provisions the GCS state bucket, Workload Identity Federation pool/provider, and Terraform service account that all other environments depend on. It runs once manually with local state.

## ADDED Requirements

### REQ-BOOT-001: State Bucket Provisioning

The bootstrap module **MUST** create a GCS bucket for Terraform remote state using `google_storage_bucket`. The bucket name **MUST** be derived from `var.project_id` (e.g., `${var.project_id}-terraform-state`). The bucket **MUST** have versioning enabled and `uniform_bucket_level_access = true`. The bucket **MUST** set `force_destroy = false` to prevent accidental state deletion.

#### Scenario: State bucket created with correct configuration · `code-based` · `critical`

- **WHEN** `terraform apply` is run in `terraform/bootstrap/`
- **THEN** a `google_storage_bucket` resource named `terraform_state` exists with `versioning.enabled = true`, `uniform_bucket_level_access = true`, and `force_destroy = false`

#### Scenario: State bucket name derived from project_id · `code-based` · `critical`

- **GIVEN** `var.project_id = "my-gcp-project"`
- **WHEN** the bootstrap module is applied
- **THEN** the bucket name is `"my-gcp-project-terraform-state"`

---

### REQ-BOOT-002: Workload Identity Federation Pool and Provider

The bootstrap module **MUST** create a `google_iam_workload_identity_pool` and a `google_iam_workload_identity_pool_provider` for GitHub Actions OIDC authentication. The provider **MUST** use `issuer_uri = "https://token.actions.githubusercontent.com"`. The provider **MUST** include an `attribute_condition` restricting access to the configured `var.github_org` and `var.github_repo`. The provider **MUST** map `google.subject`, `attribute.actor`, `attribute.repository`, and `attribute.ref` from OIDC assertions.

#### Scenario: WIF pool created for GitHub Actions · `code-based` · `critical`

- **WHEN** `terraform apply` is run in `terraform/bootstrap/`
- **THEN** a `google_iam_workload_identity_pool` resource exists with `display_name` containing "GitHub"

#### Scenario: WIF provider restricts to configured repo · `code-based` · `critical`

- **GIVEN** `var.github_org = "myorg"` and `var.github_repo = "ProyectoAgentero"`
- **WHEN** the bootstrap module is applied
- **THEN** the `google_iam_workload_identity_pool_provider` resource contains an `attribute_condition` referencing `"myorg/ProyectoAgentero"`

#### Scenario: WIF provider rejects unrelated repository · `code-based` · `critical`

- **GIVEN** the WIF provider has `attribute_condition` restricting to `"myorg/ProyectoAgentero"`
- **WHEN** a GitHub Actions workflow from `"otherorg/OtherRepo"` attempts to authenticate
- **THEN** the OIDC token exchange is denied by GCP because the assertion does not match the `attribute_condition`

---

### REQ-BOOT-003: Terraform Service Account

The bootstrap module **MUST** create a `google_service_account` for Terraform automation. The bootstrap module **MUST** create a `google_service_account_iam_member` granting `roles/iam.workloadIdentityUser` to the WIF pool's GitHub principal set. The Terraform SA **MUST NOT** have a downloadable key; authentication **MUST** occur exclusively via WIF.

#### Scenario: Terraform SA created and bound to WIF · `code-based` · `critical`

- **WHEN** `terraform apply` is run in `terraform/bootstrap/`
- **THEN** a `google_service_account` with `account_id = "terraform"` exists AND a `google_service_account_iam_member` grants `roles/iam.workloadIdentityUser` to a `principalSet://` member derived from the WIF pool

#### Scenario: No service account key resources exist · `code-based` · `critical`

- **WHEN** the bootstrap module HCL is inspected
- **THEN** no `google_service_account_key` resource is declared in any `.tf` file under `terraform/bootstrap/`

---

### REQ-BOOT-004: Standard Module Structure

The bootstrap directory **MUST** contain exactly `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf`. The `versions.tf` **MUST** declare `required_version = ">= 1.5"` and require `hashicorp/google ~> 6.0`.

#### Scenario: Bootstrap directory contains required files · `code-based` · `critical`

- **WHEN** the contents of `terraform/bootstrap/` are listed
- **THEN** the files `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf` exist

#### Scenario: Bootstrap versions.tf declares correct providers · `code-based` · `critical`

- **WHEN** `terraform/bootstrap/versions.tf` is read
- **THEN** it contains `required_version = ">= 1.5"` and a `google` provider source `"hashicorp/google"` with version `"~> 6.0"`

---

### REQ-BOOT-005: Bootstrap Outputs

The bootstrap module **MUST** output `state_bucket_name`, `wif_provider_name`, and `terraform_sa_email` so downstream environments can reference them.

#### Scenario: Bootstrap outputs defined · `code-based` · `critical`

- **WHEN** `terraform/bootstrap/outputs.tf` is read
- **THEN** it declares `output "state_bucket_name"`, `output "wif_provider_name"`, and `output "terraform_sa_email"`

---

### REQ-BOOT-006: Bootstrap Variables Parameterization

The bootstrap module **MUST** accept `var.project_id`, `var.region`, `var.github_org`, and `var.github_repo` as input variables. No values **MUST** be hardcoded for these parameters in `main.tf`.

#### Scenario: Required variables declared · `code-based` · `critical`

- **WHEN** `terraform/bootstrap/variables.tf` is read
- **THEN** it declares `variable "project_id"`, `variable "region"`, `variable "github_org"`, and `variable "github_repo"`

#### Scenario: No hardcoded project ID in main.tf · `code-based` · `critical`

- **WHEN** `terraform/bootstrap/main.tf` is searched for string literals matching a GCP project ID pattern (e.g., a literal project ID like `"my-project-123"`)
- **THEN** no hardcoded project IDs are found; all project references use `var.project_id`

---

## MODIFIED Requirements

(none)

## REMOVED Requirements

(none)

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-BOOT-001   | ADDED | MUST     | 2         |
| REQ-BOOT-002   | ADDED | MUST     | 3         |
| REQ-BOOT-003   | ADDED | MUST     | 2         |
| REQ-BOOT-004   | ADDED | MUST     | 2         |
| REQ-BOOT-005   | ADDED | MUST     | 1         |
| REQ-BOOT-006   | ADDED | MUST     | 2         |

**Total Requirements**: 6
**Total Scenarios**: 12

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-BOOT-001 > State bucket created with correct configuration | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-001 > State bucket name derived from project_id | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-002 > WIF pool created for GitHub Actions | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-002 > WIF provider restricts to configured repo | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-002 > WIF provider rejects unrelated repository | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-003 > Terraform SA created and bound to WIF | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-003 > No service account key resources exist | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-004 > Bootstrap directory contains required files | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-004 > Bootstrap versions.tf declares correct providers | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-005 > Bootstrap outputs defined | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-006 > Required variables declared | code-based | critical | pass^3 = 1.00 |
| REQ-BOOT-006 > No hardcoded project ID in main.tf | code-based | critical | pass^3 = 1.00 |
