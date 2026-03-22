# Delta Spec: Storage

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The storage module provisions a GCS document bucket with lifecycle rules, an Artifact Registry repository for Docker images, and bucket-level IAM bindings. The document bucket follows the reference patterns from `docs/terraform-gcp.md` with tiered storage lifecycle (Standard -> Nearline -> Coldline -> Delete).

## ADDED Requirements

### REQ-STOR-001: Document Storage Bucket

The storage module **MUST** create a `google_storage_bucket` for document storage. The bucket **MUST** set `uniform_bucket_level_access = true` and `public_access_prevention = "enforced"`. The bucket **MUST** enable versioning. The bucket name **MUST** be derived from `var.project_id`.

#### Scenario: Document bucket created with security settings · `code-based` · `critical`

- **WHEN** `terraform/modules/storage/main.tf` is read
- **THEN** a `google_storage_bucket` resource exists with `uniform_bucket_level_access = true` and `public_access_prevention = "enforced"`

#### Scenario: Bucket versioning enabled · `code-based` · `critical`

- **WHEN** the document bucket resource is inspected
- **THEN** `versioning.enabled = true`

---

### REQ-STOR-002: Lifecycle Rules

The storage module **SHOULD** configure lifecycle rules for tiered storage transitions (Standard to Nearline after 30 days, Nearline to Coldline after 90 days) and cleanup of old versions.

#### Scenario: Lifecycle rule transitions to Nearline · `code-based` · `standard`

- **WHEN** the document bucket's `lifecycle_rule` blocks are inspected
- **THEN** at least one rule sets `action.type = "SetStorageClass"` with `action.storage_class = "NEARLINE"` and `condition.age = 30`

#### Scenario: Old versions cleaned up · `code-based` · `standard`

- **WHEN** the lifecycle rules are inspected
- **THEN** at least one rule deletes objects when `condition.num_newer_versions` exceeds a threshold

---

### REQ-STOR-003: Artifact Registry Repository

The storage module **MUST** create a `google_artifact_registry_repository` for Docker images with `format = "DOCKER"`. The repository location **MUST** be parameterized via `var.region`.

#### Scenario: Artifact Registry repo created · `code-based` · `critical`

- **WHEN** `terraform/modules/storage/main.tf` is read
- **THEN** a `google_artifact_registry_repository` resource exists with `format = "DOCKER"`

#### Scenario: Artifact Registry region parameterized · `code-based` · `critical`

- **WHEN** the `google_artifact_registry_repository` resource is inspected
- **THEN** `location = var.region`

---

### REQ-STOR-004: Bucket IAM

The storage module **MUST** create `google_storage_bucket_iam_member` resources granting the API and worker SAs `roles/storage.objectUser` on the document bucket. The module **MUST NOT** grant public access to the bucket.

#### Scenario: SA gets objectUser on document bucket · `code-based` · `critical`

- **WHEN** `terraform/modules/storage/main.tf` is read
- **THEN** at least one `google_storage_bucket_iam_member` resource grants `roles/storage.objectUser` referencing an SA email variable

#### Scenario: No public bucket access · `code-based` · `critical`

- **WHEN** all `google_storage_bucket_iam_member` resources in the storage module are inspected
- **THEN** no resource has `member = "allUsers"` or `member = "allAuthenticatedUsers"`

---

### REQ-STOR-005: Storage Module Outputs

The storage module **MUST** output `bucket_name`, `bucket_url`, and `registry_url`.

#### Scenario: Storage outputs declared · `code-based` · `critical`

- **WHEN** `terraform/modules/storage/outputs.tf` is read
- **THEN** it declares `output "bucket_name"`, `output "bucket_url"`, and `output "registry_url"`

---

### REQ-STOR-006: Storage Module Structure

The storage module **MUST** contain `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf`.

#### Scenario: Storage module files present · `code-based` · `critical`

- **WHEN** the contents of `terraform/modules/storage/` are listed
- **THEN** `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf` exist

---

## MODIFIED Requirements

(none)

## REMOVED Requirements

(none)

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-STOR-001   | ADDED | MUST     | 2         |
| REQ-STOR-002   | ADDED | SHOULD   | 2         |
| REQ-STOR-003   | ADDED | MUST     | 2         |
| REQ-STOR-004   | ADDED | MUST     | 2         |
| REQ-STOR-005   | ADDED | MUST     | 1         |
| REQ-STOR-006   | ADDED | MUST     | 1         |

**Total Requirements**: 6
**Total Scenarios**: 10

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-STOR-001 > Document bucket created with security settings | code-based | critical | pass^3 = 1.00 |
| REQ-STOR-001 > Bucket versioning enabled | code-based | critical | pass^3 = 1.00 |
| REQ-STOR-002 > Lifecycle rule transitions to Nearline | code-based | standard | pass@3 >= 0.90 |
| REQ-STOR-002 > Old versions cleaned up | code-based | standard | pass@3 >= 0.90 |
| REQ-STOR-003 > Artifact Registry repo created | code-based | critical | pass^3 = 1.00 |
| REQ-STOR-003 > Artifact Registry region parameterized | code-based | critical | pass^3 = 1.00 |
| REQ-STOR-004 > SA gets objectUser on document bucket | code-based | critical | pass^3 = 1.00 |
| REQ-STOR-004 > No public bucket access | code-based | critical | pass^3 = 1.00 |
| REQ-STOR-005 > Storage outputs declared | code-based | critical | pass^3 = 1.00 |
| REQ-STOR-006 > Storage module files present | code-based | critical | pass^3 = 1.00 |
