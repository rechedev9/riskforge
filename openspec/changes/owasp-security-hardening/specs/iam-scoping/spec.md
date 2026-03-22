# Delta Spec: IAM Scoping

**Change**: owasp-security-hardening
**Date**: 2026-03-22T15:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

Two IAM least-privilege gaps are addressed here. H2: `roles/secretmanager.secretAccessor` is currently granted at project scope for both the API and Worker service accounts via the `sa_project_roles` local in `modules/iam/main.tf` line 34. This grants both SAs access to every secret in the project. H3: The Terraform SA in `terraform/bootstrap/main.tf` has no project-level IAM role assignments in IaC at all — its permissions are undocumented and uncontrolled. Both are addressed in this spec.

## ADDED Requirements

### REQ-IAM-001: Terraform SA Minimum-Role Bindings in Bootstrap

`terraform/bootstrap/main.tf` **MUST** contain `google_project_iam_member` resources granting the Terraform SA (`google_service_account.terraform.email`) an explicitly enumerated set of admin roles.

The role list **MUST** include at minimum: `roles/run.admin`, `roles/spanner.admin`, `roles/storage.admin`, `roles/artifactregistry.admin`, `roles/iam.serviceAccountAdmin`, `roles/iam.workloadIdentityPoolAdmin`, `roles/compute.networkAdmin`, `roles/monitoring.admin`, `roles/secretmanager.admin`, `roles/pubsub.admin`, `roles/logging.admin`.

The Terraform SA **MUST NOT** be granted `roles/editor` or `roles/owner` — broad primitive roles are prohibited.

#### Scenario: Bootstrap plan shows SA role bindings · `code-based` · `critical`

- **WHEN** `terraform plan -no-color` is run in `terraform/bootstrap/`
- **THEN** the plan output contains at least 11 `google_project_iam_member` resources for `serviceAccount:terraform@<project>.iam.gserviceaccount.com`

#### Scenario: roles/editor is absent from bootstrap · `code-based` · `critical`

- **WHEN** `terraform/bootstrap/main.tf` is searched for the string `roles/editor`
- **THEN** no match is found in any `google_project_iam_member` resource block targeting the Terraform SA

#### Scenario: roles/owner is absent from bootstrap · `code-based` · `critical`

- **WHEN** `terraform/bootstrap/main.tf` is searched for the string `roles/owner`
- **THEN** no match is found in any `google_project_iam_member` resource block targeting the Terraform SA

### REQ-IAM-002: Per-Secret IAM Binding Variable Stub in IAM Module

`modules/iam/main.tf` (or a sibling `modules/iam/variables.tf`) **MUST** declare a `secret_bindings` variable of type `map(object({ sa_key = string }))` with `default = {}`.

The IAM module **MUST** contain a `google_secret_manager_secret_iam_member` resource driven by `for_each = var.secret_bindings` that is ready to bind per-secret access when secrets are added to IaC.

The resource block **MUST** include a code comment explaining it replaces the former project-scoped `secretAccessor` grant.

#### Scenario: secret_bindings variable declared with empty default · `code-based` · `critical`

- **WHEN** `modules/iam/variables.tf` is inspected
- **THEN** a `variable "secret_bindings"` block exists with `default = {}` and type `map(object(...))`

#### Scenario: Empty secret_bindings produces no plan changes · `code-based` · `critical`

- **GIVEN** both dev and prod environment `main.tf` files do not pass `secret_bindings` (so the default `{}` applies)
- **WHEN** `terraform plan` is run in `terraform/environments/dev/`
- **THEN** no `google_secret_manager_secret_iam_member` resources appear in the plan output

---

## MODIFIED Requirements

### REQ-IAM-003: Remove Project-Scoped secretAccessor from sa_project_roles

**Previously**: `modules/iam/main.tf` line 34 included `"roles/secretmanager.secretAccessor"` in the `sa_project_roles` local, granting both the API and Worker SAs project-wide Secret Manager access.

The `sa_project_roles` local **MUST NOT** include `"roles/secretmanager.secretAccessor"`.

The remaining project-level roles in the list (`roles/logging.logWriter`, `roles/cloudtrace.agent`) **MUST** remain unchanged.

#### Scenario: secretAccessor absent from sa_project_roles · `code-based` · `critical`

- **WHEN** `modules/iam/main.tf` is inspected
- **THEN** the string `"roles/secretmanager.secretAccessor"` does not appear in the `sa_project_roles` local value list

#### Scenario: Remaining roles still present · `code-based` · `critical`

- **WHEN** `modules/iam/main.tf` is inspected
- **THEN** the `sa_project_roles` local list still contains `"roles/logging.logWriter"` and `"roles/cloudtrace.agent"`

#### Scenario: Plan shows destroy of old secretAccessor bindings · `code-based` · `critical`

- **GIVEN** the state contains `google_project_iam_member` resources for the `secretmanager.secretAccessor` role on both SAs
- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output shows `# module.iam.google_project_iam_member.sa_roles["api-secretmanager.secretAccessor"] will be destroyed` (and the analogous worker entry)

---

## Acceptance Criteria Summary

| Requirement ID | Type     | Priority | Scenarios |
|----------------|----------|----------|-----------|
| REQ-IAM-001    | ADDED    | MUST     | 3         |
| REQ-IAM-002    | ADDED    | MUST     | 2         |
| REQ-IAM-003    | MODIFIED | MUST     | 3         |

**Total Requirements**: 3
**Total Scenarios**: 8

---

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-IAM-001 › Bootstrap plan shows SA role bindings | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-001 › roles/editor is absent from bootstrap | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-001 › roles/owner is absent from bootstrap | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-002 › secret_bindings variable declared with empty default | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-002 › Empty secret_bindings produces no plan changes | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-003 › secretAccessor absent from sa_project_roles | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-003 › Remaining roles still present | code-based | critical | pass^3 = 1.00 |
| REQ-IAM-003 › Plan shows destroy of old secretAccessor bindings | code-based | critical | pass^3 = 1.00 |
