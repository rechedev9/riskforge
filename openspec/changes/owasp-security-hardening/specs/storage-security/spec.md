# Delta Spec: Storage Security

**Change**: owasp-security-hardening
**Date**: 2026-03-22T15:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

Three storage-layer security gaps are addressed. M3: `terraform/bootstrap/main.tf` creates `google_storage_bucket.terraform_state` without `public_access_prevention = "enforced"`, leaving open the possibility of a bucket ACL misconfiguration exposing Terraform state (which contains sensitive infrastructure metadata). H5: `modules/storage/main.tf` creates `google_artifact_registry_repository.docker` but assigns no IAM roles — any SA with project-level permissions can push or pull images. L1: Artifact Registry has no vulnerability scanning policy. The documents GCS bucket at line 1–30 of `modules/storage/main.tf` already has `public_access_prevention = "enforced"` — no change needed there.

## ADDED Requirements

### REQ-STORAGE-001: public_access_prevention on Terraform State Bucket

`terraform/bootstrap/main.tf` **MUST** add `public_access_prevention = "enforced"` to the `google_storage_bucket.terraform_state` resource.

#### Scenario: State bucket plan shows public_access_prevention enforced · `code-based` · `critical`

- **WHEN** `terraform plan -no-color` is run in `terraform/bootstrap/`
- **THEN** the plan output shows `~ google_storage_bucket.terraform_state` with `+ public_access_prevention = "enforced"` (an update, not a destroy/recreate)

#### Scenario: State bucket resource contains the attribute · `code-based` · `critical`

- **WHEN** the `google_storage_bucket.terraform_state` resource block in `terraform/bootstrap/main.tf` is inspected
- **THEN** it contains `public_access_prevention = "enforced"`

### REQ-STORAGE-002: Artifact Registry IAM — API SA Writer

`modules/storage/main.tf` **MUST** contain a `google_artifact_registry_repository_iam_member` resource granting `roles/artifactregistry.writer` to `var.api_sa_email` on `google_artifact_registry_repository.docker`.

#### Scenario: API SA writer binding present in plan · `code-based` · `critical`

- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output contains `module.storage.google_artifact_registry_repository_iam_member.api_writer` with `role = "roles/artifactregistry.writer"` and `member = "serviceAccount:<api-sa-email>"`

### REQ-STORAGE-003: Artifact Registry IAM — Worker SA Writer

`modules/storage/main.tf` **MUST** contain a `google_artifact_registry_repository_iam_member` resource granting `roles/artifactregistry.writer` to `var.worker_sa_email` on `google_artifact_registry_repository.docker`.

#### Scenario: Worker SA writer binding present in plan · `code-based` · `critical`

- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output contains `module.storage.google_artifact_registry_repository_iam_member.worker_writer` with `role = "roles/artifactregistry.writer"` and `member = "serviceAccount:<worker-sa-email>"`

### REQ-STORAGE-004: Artifact Registry IAM — Cloud Run Runtime SA Reader

`modules/storage/main.tf` **MUST** accept a `runtime_sa_email` variable and **MUST** contain a `google_artifact_registry_repository_iam_member` resource granting `roles/artifactregistry.reader` to that SA on `google_artifact_registry_repository.docker`.

The `runtime_sa_email` variable **MUST** have a description and **SHOULD** have a non-empty validation or default of `""` (making it optional; the IAM resource **MAY** use `count = var.runtime_sa_email != "" ? 1 : 0` to conditionally create the binding).

#### Scenario: Reader binding present in plan when runtime_sa_email is set · `code-based` · `critical`

- **GIVEN** the environment passes a non-empty `runtime_sa_email` to the storage module
- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output contains `module.storage.google_artifact_registry_repository_iam_member.runtime_reader` with `role = "roles/artifactregistry.reader"`

#### Scenario: No reader binding when runtime_sa_email is empty · `code-based` · `standard`

- **GIVEN** the environment does not pass `runtime_sa_email` (defaults to `""`)
- **WHEN** `terraform plan -no-color` is run
- **THEN** no `google_artifact_registry_repository_iam_member` resource for `roles/artifactregistry.reader` appears in the plan

### REQ-STORAGE-005: Artifact Registry Vulnerability Scanning Enabled

`modules/storage/main.tf` **MUST** add a `vulnerability_scanning_config` block to `google_artifact_registry_repository.docker` with `enablement_config = "INHERITED"`.

#### Scenario: Vulnerability scanning config present in plan · `code-based` · `standard`

- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output shows `~ module.storage.google_artifact_registry_repository.docker` with `vulnerability_scanning_config` set to `enablement_config = "INHERITED"`

#### Scenario: Scanning config in HCL · `code-based` · `standard`

- **WHEN** the `google_artifact_registry_repository.docker` block in `modules/storage/main.tf` is inspected
- **THEN** a `vulnerability_scanning_config { enablement_config = "INHERITED" }` block is present

---

## Acceptance Criteria Summary

| Requirement ID  | Type  | Priority | Scenarios |
|-----------------|-------|----------|-----------|
| REQ-STORAGE-001 | ADDED | MUST     | 2         |
| REQ-STORAGE-002 | ADDED | MUST     | 1         |
| REQ-STORAGE-003 | ADDED | MUST     | 1         |
| REQ-STORAGE-004 | ADDED | MUST     | 2         |
| REQ-STORAGE-005 | ADDED | SHOULD   | 2         |

**Total Requirements**: 5
**Total Scenarios**: 8

---

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-STORAGE-001 › State bucket plan shows public_access_prevention enforced | code-based | critical | pass^3 = 1.00 |
| REQ-STORAGE-001 › State bucket resource contains the attribute | code-based | critical | pass^3 = 1.00 |
| REQ-STORAGE-002 › API SA writer binding present in plan | code-based | critical | pass^3 = 1.00 |
| REQ-STORAGE-003 › Worker SA writer binding present in plan | code-based | critical | pass^3 = 1.00 |
| REQ-STORAGE-004 › Reader binding present in plan when runtime_sa_email is set | code-based | critical | pass^3 = 1.00 |
| REQ-STORAGE-004 › No reader binding when runtime_sa_email is empty | code-based | standard | pass@3 ≥ 0.90 |
| REQ-STORAGE-005 › Vulnerability scanning config present in plan | code-based | standard | pass@3 ≥ 0.90 |
| REQ-STORAGE-005 › Scanning config in HCL | code-based | standard | pass@3 ≥ 0.90 |
