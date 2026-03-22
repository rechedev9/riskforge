# Delta Spec: Audit Logging

**Change**: owasp-security-hardening
**Date**: 2026-03-22T15:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

No `google_project_iam_audit_config` resource exists anywhere in the Terraform codebase. Admin and data-access operations against all GCP services are silently unlogged, making incident reconstruction impossible. This spec covers C3: adding project-level audit logging for `allServices` covering `DATA_READ`, `DATA_WRITE`, and `ADMIN_READ` log types in `modules/iam/main.tf`.

## ADDED Requirements

### REQ-AUDITLOG-001: Project-Level Audit Config Resource

The `modules/iam/main.tf` file **MUST** contain a `google_project_iam_audit_config` resource targeting the `project` variable with `service = "allServices"`.

The resource **MUST** declare three `audit_log_config` blocks: one for `DATA_READ`, one for `DATA_WRITE`, and one for `ADMIN_READ`.

The resource **MUST NOT** include an `exempted_members` list in any `audit_log_config` block — no principals are to be excluded from audit coverage at this stage.

#### Scenario: Audit config resource present in plan · `code-based` · `critical`

- **WHEN** `terraform plan` is run in `terraform/environments/dev/` after the change
- **THEN** the plan output contains `# module.iam.google_project_iam_audit_config.default will be created` with `service = "allServices"`

#### Scenario: All three log types declared · `code-based` · `critical`

- **WHEN** `terraform validate` is run in `terraform/environments/dev/`
- **THEN** the command exits with code `0` and the HCL file at `terraform/modules/iam/main.tf` contains exactly three `audit_log_config` blocks with `log_type` values `"DATA_READ"`, `"DATA_WRITE"`, and `"ADMIN_READ"`

#### Scenario: No exemptions present · `code-based` · `critical`

- **GIVEN** the `google_project_iam_audit_config` resource block is written
- **WHEN** the file is inspected for `exempted_members`
- **THEN** no `exempted_members` attribute appears in any `audit_log_config` block within that resource

### REQ-AUDITLOG-002: Audit Config Passes Format Check

The `modules/iam/main.tf` file after the change **MUST** pass `terraform fmt -check` without modifications.

#### Scenario: Format check clean · `code-based` · `critical`

- **WHEN** `terraform fmt -check -recursive terraform/` is run from the repository root
- **THEN** the command exits with code `0` and produces no output listing `modules/iam/main.tf`

### REQ-AUDITLOG-003: No Existing Resources Destroyed

Adding the audit config **MUST NOT** cause `terraform plan` to show any `destroy` operations on resources already present in the current state.

#### Scenario: Plan shows only additions · `code-based` · `critical`

- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the summary line reads `Plan: N to add, 0 to change, 0 to destroy` (N >= 1) with no destroy operations listed for pre-existing IAM or service-account resources

---

## Acceptance Criteria Summary

| Requirement ID    | Type  | Priority | Scenarios |
|-------------------|-------|----------|-----------|
| REQ-AUDITLOG-001  | ADDED | MUST     | 3         |
| REQ-AUDITLOG-002  | ADDED | MUST     | 1         |
| REQ-AUDITLOG-003  | ADDED | MUST     | 1         |

**Total Requirements**: 3
**Total Scenarios**: 5

---

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-AUDITLOG-001 › Audit config resource present in plan | code-based | critical | pass^3 = 1.00 |
| REQ-AUDITLOG-001 › All three log types declared | code-based | critical | pass^3 = 1.00 |
| REQ-AUDITLOG-001 › No exemptions present | code-based | critical | pass^3 = 1.00 |
| REQ-AUDITLOG-002 › Format check clean | code-based | critical | pass^3 = 1.00 |
| REQ-AUDITLOG-003 › Plan shows only additions | code-based | critical | pass^3 = 1.00 |
