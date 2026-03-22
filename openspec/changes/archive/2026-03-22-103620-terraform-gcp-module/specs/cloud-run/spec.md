# Delta Spec: Cloud Run

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The cloud-run module is a generic, reusable module that provisions a single `google_cloud_run_v2_service` and its resource-level IAM bindings. It is instantiated twice per environment -- once for the API (public ingress) and once for the worker (internal-only ingress with Pub/Sub invoker). The module uses dynamic blocks for environment variables and secret references.

## ADDED Requirements

### REQ-CRUN-001: Cloud Run V2 Service

The cloud-run module **MUST** create a `google_cloud_run_v2_service` resource (v2 API, not the deprecated `google_cloud_run_service`). The service **MUST** parameterize `name`, `location`, `ingress`, and `deletion_protection` via input variables.

#### Scenario: Cloud Run v2 service resource used · `code-based` · `critical`

- **WHEN** `terraform/modules/cloud-run/main.tf` is read
- **THEN** a `google_cloud_run_v2_service` resource exists (not `google_cloud_run_service`)

#### Scenario: Service name parameterized · `code-based` · `critical`

- **WHEN** the `google_cloud_run_v2_service` resource is inspected
- **THEN** `name = var.service_name` and `location = var.region`

---

### REQ-CRUN-002: Scaling Configuration

The cloud-run module **MUST** configure `scaling.min_instance_count = var.min_instances` and `scaling.max_instance_count = var.max_instances` in the service template. This enables dev to scale to zero (`min_instances = 0`) while prod maintains warm instances (`min_instances = 2`).

#### Scenario: Scaling uses variable inputs · `code-based` · `critical`

- **WHEN** the `google_cloud_run_v2_service` template block is inspected
- **THEN** `scaling.min_instance_count = var.min_instances` and `scaling.max_instance_count = var.max_instances`

---

### REQ-CRUN-003: Container Image from Artifact Registry

The cloud-run module **MUST** accept a `var.image` input for the container image reference. The image reference **MUST NOT** be hardcoded.

#### Scenario: Container image parameterized · `code-based` · `critical`

- **WHEN** the `containers` block is inspected
- **THEN** `image = var.image` (or a variable-derived expression)

---

### REQ-CRUN-004: Dynamic Environment Variables

The cloud-run module **MUST** use `dynamic "env"` blocks to inject plain environment variables from `var.env_vars` and secret-backed environment variables from `var.secret_env_vars`. This enables the environment layer to pass different env vars to the API vs. worker instances.

#### Scenario: Dynamic env block for plain variables · `code-based` · `critical`

- **WHEN** `terraform/modules/cloud-run/main.tf` is read
- **THEN** a `dynamic "env"` block iterates over `var.env_vars`

#### Scenario: Dynamic env block for secret variables · `code-based` · `critical`

- **WHEN** `terraform/modules/cloud-run/main.tf` is read
- **THEN** a `dynamic "env"` block iterates over `var.secret_env_vars` and uses `value_source.secret_key_ref`

---

### REQ-CRUN-005: VPC Access

The cloud-run module **MUST** configure `vpc_access.connector = var.vpc_connector_id` in the template. The egress **SHOULD** default to `"PRIVATE_RANGES_ONLY"`.

#### Scenario: VPC connector wired from variable · `code-based` · `critical`

- **WHEN** the `vpc_access` block is inspected
- **THEN** `connector = var.vpc_connector_id`

---

### REQ-CRUN-006: Resource-Level IAM

The cloud-run module **MUST** support configurable IAM: public access (API) via `google_cloud_run_v2_service_iam_member` with `member = "allUsers"` and `role = "roles/run.invoker"`, or restricted access (worker) via a specific SA member. The module **MUST** accept an `allow_unauthenticated` variable (or equivalent) to control this behavior.

#### Scenario: Public API gets allUsers invoker binding · `code-based` · `critical`

- **GIVEN** the module is instantiated with `allow_unauthenticated = true` (or equivalent flag)
- **WHEN** the IAM resources are inspected
- **THEN** a `google_cloud_run_v2_service_iam_member` resource grants `roles/run.invoker` to `"allUsers"`

#### Scenario: Worker gets SA-only invoker binding · `code-based` · `critical`

- **GIVEN** the module is instantiated with `allow_unauthenticated = false` and an `invoker_sa_email` is provided
- **WHEN** the IAM resources are inspected
- **THEN** a `google_cloud_run_v2_service_iam_member` resource grants `roles/run.invoker` to `"serviceAccount:${var.invoker_sa_email}"`

---

### REQ-CRUN-007: Cloud Run Module Outputs

The cloud-run module **MUST** output `service_url`, `service_name`, and `service_id`.

#### Scenario: Cloud Run outputs declared · `code-based` · `critical`

- **WHEN** `terraform/modules/cloud-run/outputs.tf` is read
- **THEN** it declares `output "service_url"`, `output "service_name"`, and `output "service_id"`

---

### REQ-CRUN-008: Cloud Run Module Structure

The cloud-run module **MUST** contain `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf`. The `versions.tf` **MUST** declare `hashicorp/google ~> 6.0`.

#### Scenario: Cloud Run module files present · `code-based` · `critical`

- **WHEN** the contents of `terraform/modules/cloud-run/` are listed
- **THEN** `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf` exist

---

### REQ-CRUN-009: Service Account Assignment

The cloud-run module **MUST** set `template.service_account = var.service_account_email` to run the container under a dedicated SA rather than the default compute SA.

#### Scenario: Service account assigned from variable · `code-based` · `critical`

- **WHEN** the `template` block is inspected
- **THEN** `service_account = var.service_account_email`

---

### REQ-CRUN-010: Ingress Configuration

The cloud-run module **MUST** accept `var.ingress` to control traffic source. The API instance **MUST** use `"INGRESS_TRAFFIC_ALL"` and the worker **MUST** use `"INGRESS_TRAFFIC_INTERNAL_ONLY"`. This is set at the environment layer, not hardcoded in the module.

#### Scenario: Ingress parameterized · `code-based` · `critical`

- **WHEN** the `google_cloud_run_v2_service` resource is inspected
- **THEN** `ingress = var.ingress`

---

## MODIFIED Requirements

(none)

## REMOVED Requirements

(none)

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-CRUN-001   | ADDED | MUST     | 2         |
| REQ-CRUN-002   | ADDED | MUST     | 1         |
| REQ-CRUN-003   | ADDED | MUST     | 1         |
| REQ-CRUN-004   | ADDED | MUST     | 2         |
| REQ-CRUN-005   | ADDED | MUST     | 1         |
| REQ-CRUN-006   | ADDED | MUST     | 2         |
| REQ-CRUN-007   | ADDED | MUST     | 1         |
| REQ-CRUN-008   | ADDED | MUST     | 1         |
| REQ-CRUN-009   | ADDED | MUST     | 1         |
| REQ-CRUN-010   | ADDED | MUST     | 1         |

**Total Requirements**: 10
**Total Scenarios**: 13

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-CRUN-001 > Cloud Run v2 service resource used | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-001 > Service name parameterized | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-002 > Scaling uses variable inputs | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-003 > Container image parameterized | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-004 > Dynamic env block for plain variables | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-004 > Dynamic env block for secret variables | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-005 > VPC connector wired from variable | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-006 > Public API gets allUsers invoker binding | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-006 > Worker gets SA-only invoker binding | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-007 > Cloud Run outputs declared | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-008 > Cloud Run module files present | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-009 > Service account assigned from variable | code-based | critical | pass^3 = 1.00 |
| REQ-CRUN-010 > Ingress parameterized | code-based | critical | pass^3 = 1.00 |
