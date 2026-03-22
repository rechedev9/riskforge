# Delta Spec: Spanner

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The Spanner module provisions a Cloud Spanner instance, a database with initial DDL for the Carriers and AppetiteRules tables, and database-level IAM bindings granting the API and worker service accounts access. Spanner uses `processing_units` (not `num_nodes`) for fine-grained capacity control.

## ADDED Requirements

### REQ-SPAN-001: Spanner Instance

The Spanner module **MUST** create a `google_spanner_instance` with `config = "regional-${var.region}"`. The instance **MUST** use `processing_units = var.spanner_processing_units` (not `num_nodes`). The instance **MUST** set `force_destroy = false`.

#### Scenario: Spanner instance uses processing_units · `code-based` · `critical`

- **WHEN** `terraform/modules/spanner/main.tf` is read
- **THEN** a `google_spanner_instance` resource exists with `processing_units = var.spanner_processing_units` and does NOT use `num_nodes`

#### Scenario: Spanner instance regional config parameterized · `code-based` · `critical`

- **WHEN** the `google_spanner_instance` resource is inspected
- **THEN** `config` references `var.region` (e.g., `"regional-${var.region}"`)

---

### REQ-SPAN-002: Spanner Database with DDL

The Spanner module **MUST** create a `google_spanner_database` with initial DDL containing `CREATE TABLE Carriers` and `CREATE TABLE AppetiteRules` statements. The database **MUST** set `deletion_protection = var.deletion_protection` to allow environment-specific control. The database **SHOULD** set `version_retention_period` for point-in-time recovery.

#### Scenario: Database DDL contains Carriers table · `code-based` · `critical`

- **WHEN** `terraform/modules/spanner/main.tf` is read
- **THEN** the `google_spanner_database` resource's `ddl` list contains a string matching `CREATE TABLE Carriers`

#### Scenario: Database DDL contains AppetiteRules table · `code-based` · `critical`

- **WHEN** `terraform/modules/spanner/main.tf` is read
- **THEN** the `google_spanner_database` resource's `ddl` list contains a string matching `CREATE TABLE AppetiteRules`

#### Scenario: Deletion protection parameterized · `code-based` · `critical`

- **WHEN** the `google_spanner_database` resource is inspected
- **THEN** `deletion_protection = var.deletion_protection`

---

### REQ-SPAN-003: Spanner Database IAM

The Spanner module **MUST** create `google_spanner_database_iam_binding` or `google_spanner_database_iam_member` resources granting `roles/spanner.databaseUser` to the API and worker service accounts received via input variables.

#### Scenario: Database IAM grants databaseUser to SA inputs · `code-based` · `critical`

- **WHEN** `terraform/modules/spanner/main.tf` is read
- **THEN** at least one `google_spanner_database_iam_binding` or `google_spanner_database_iam_member` resource grants `roles/spanner.databaseUser` referencing SA email variables

---

### REQ-SPAN-004: Spanner Module Outputs

The Spanner module **MUST** output `instance_name` and `database_name` for consumption by the cloud-run module (environment variables).

#### Scenario: Spanner outputs declared · `code-based` · `critical`

- **WHEN** `terraform/modules/spanner/outputs.tf` is read
- **THEN** it declares `output "instance_name"` and `output "database_name"`

---

### REQ-SPAN-005: Spanner Module Structure

The Spanner module **MUST** contain `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf`. The `versions.tf` **MUST** declare `hashicorp/google ~> 6.0`.

#### Scenario: Spanner module files present · `code-based` · `critical`

- **WHEN** the contents of `terraform/modules/spanner/` are listed
- **THEN** `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf` exist

---

### REQ-SPAN-006: Spanner Variables Parameterization

The Spanner module **MUST** accept `var.project_id`, `var.region`, `var.spanner_processing_units`, `var.deletion_protection`, and SA email variables as inputs. No resource arguments **MUST** contain hardcoded project IDs or regions.

#### Scenario: Processing units variable declared · `code-based` · `critical`

- **WHEN** `terraform/modules/spanner/variables.tf` is read
- **THEN** it declares `variable "spanner_processing_units"` and `variable "deletion_protection"`

---

## MODIFIED Requirements

(none)

## REMOVED Requirements

(none)

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-SPAN-001   | ADDED | MUST     | 2         |
| REQ-SPAN-002   | ADDED | MUST     | 3         |
| REQ-SPAN-003   | ADDED | MUST     | 1         |
| REQ-SPAN-004   | ADDED | MUST     | 1         |
| REQ-SPAN-005   | ADDED | MUST     | 1         |
| REQ-SPAN-006   | ADDED | MUST     | 1         |

**Total Requirements**: 6
**Total Scenarios**: 9

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-SPAN-001 > Spanner instance uses processing_units | code-based | critical | pass^3 = 1.00 |
| REQ-SPAN-001 > Spanner instance regional config parameterized | code-based | critical | pass^3 = 1.00 |
| REQ-SPAN-002 > Database DDL contains Carriers table | code-based | critical | pass^3 = 1.00 |
| REQ-SPAN-002 > Database DDL contains AppetiteRules table | code-based | critical | pass^3 = 1.00 |
| REQ-SPAN-002 > Deletion protection parameterized | code-based | critical | pass^3 = 1.00 |
| REQ-SPAN-003 > Database IAM grants databaseUser to SA inputs | code-based | critical | pass^3 = 1.00 |
| REQ-SPAN-004 > Spanner outputs declared | code-based | critical | pass^3 = 1.00 |
| REQ-SPAN-005 > Spanner module files present | code-based | critical | pass^3 = 1.00 |
| REQ-SPAN-006 > Processing units variable declared | code-based | critical | pass^3 = 1.00 |
