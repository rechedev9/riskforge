# Delta Spec: Provider Pinning

**Change**: owasp-security-hardening
**Date**: 2026-03-22T15:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

All `versions.tf` files in this repository use `version = "~> 6.0"` for the `hashicorp/google` provider. This pessimistic constraint allows any `6.x.x` version, meaning a minor-version release (e.g., `6.15.0` → `6.16.0`) can introduce behavioral changes silently. This spec covers M5: pinning every `versions.tf` to `~> 6.14.1` (patch-level pessimistic constraint), locking to the `6.14.x` minor series while still receiving patch updates. Affected files: `terraform/bootstrap/versions.tf` and all module `versions.tf` files (cloud-run, iam, networking, spanner, pubsub, storage, monitoring).

## MODIFIED Requirements

### REQ-PIN-001: Bootstrap versions.tf Pinned to Patch Level

**Previously**: `terraform/bootstrap/versions.tf` contained `version = "~> 6.0"` for `hashicorp/google`.

`terraform/bootstrap/versions.tf` **MUST** change the `hashicorp/google` provider constraint to `version = "~> 6.14.1"`.

The `required_version` for Terraform itself (`>= 1.5`) **MUST** remain unchanged.

#### Scenario: Bootstrap versions.tf uses ~> 6.14.1 · `code-based` · `critical`

- **WHEN** `terraform/bootstrap/versions.tf` is inspected
- **THEN** the `version` attribute for `source = "hashicorp/google"` reads `"~> 6.14.1"`

#### Scenario: Bootstrap versions.tf no longer contains ~> 6.0 · `code-based` · `critical`

- **WHEN** `terraform/bootstrap/versions.tf` is searched for the string `"~> 6.0"`
- **THEN** no match is found

### REQ-PIN-002: cloud-run Module versions.tf Pinned to Patch Level

**Previously**: `terraform/modules/cloud-run/versions.tf` contained `version = "~> 6.0"`.

`terraform/modules/cloud-run/versions.tf` **MUST** change the `hashicorp/google` provider constraint to `version = "~> 6.14.1"`.

#### Scenario: cloud-run versions.tf uses ~> 6.14.1 · `code-based` · `critical`

- **WHEN** `terraform/modules/cloud-run/versions.tf` is inspected
- **THEN** the `version` attribute for `source = "hashicorp/google"` reads `"~> 6.14.1"`

### REQ-PIN-003: iam Module versions.tf Pinned to Patch Level

**Previously**: `terraform/modules/iam/versions.tf` contained `version = "~> 6.0"`.

`terraform/modules/iam/versions.tf` **MUST** change the `hashicorp/google` provider constraint to `version = "~> 6.14.1"`.

#### Scenario: iam versions.tf uses ~> 6.14.1 · `code-based` · `critical`

- **WHEN** `terraform/modules/iam/versions.tf` is inspected
- **THEN** the `version` attribute for `source = "hashicorp/google"` reads `"~> 6.14.1"`

### REQ-PIN-004: networking Module versions.tf Pinned to Patch Level

**Previously**: `terraform/modules/networking/versions.tf` contained `version = "~> 6.0"`.

`terraform/modules/networking/versions.tf` **MUST** change the `hashicorp/google` provider constraint to `version = "~> 6.14.1"`.

#### Scenario: networking versions.tf uses ~> 6.14.1 · `code-based` · `critical`

- **WHEN** `terraform/modules/networking/versions.tf` is inspected
- **THEN** the `version` attribute for `source = "hashicorp/google"` reads `"~> 6.14.1"`

### REQ-PIN-005: spanner Module versions.tf Pinned to Patch Level

**Previously**: `terraform/modules/spanner/versions.tf` contained `version = "~> 6.0"`.

`terraform/modules/spanner/versions.tf` **MUST** change the `hashicorp/google` provider constraint to `version = "~> 6.14.1"`.

#### Scenario: spanner versions.tf uses ~> 6.14.1 · `code-based` · `critical`

- **WHEN** `terraform/modules/spanner/versions.tf` is inspected
- **THEN** the `version` attribute for `source = "hashicorp/google"` reads `"~> 6.14.1"`

### REQ-PIN-006: pubsub Module versions.tf Pinned to Patch Level

**Previously**: `terraform/modules/pubsub/versions.tf` contained `version = "~> 6.0"`.

`terraform/modules/pubsub/versions.tf` **MUST** change the `hashicorp/google` provider constraint to `version = "~> 6.14.1"`.

#### Scenario: pubsub versions.tf uses ~> 6.14.1 · `code-based` · `critical`

- **WHEN** `terraform/modules/pubsub/versions.tf` is inspected
- **THEN** the `version` attribute for `source = "hashicorp/google"` reads `"~> 6.14.1"`

### REQ-PIN-007: storage Module versions.tf Pinned to Patch Level

**Previously**: `terraform/modules/storage/versions.tf` contained `version = "~> 6.0"`.

`terraform/modules/storage/versions.tf` **MUST** change the `hashicorp/google` provider constraint to `version = "~> 6.14.1"`.

#### Scenario: storage versions.tf uses ~> 6.14.1 · `code-based` · `critical`

- **WHEN** `terraform/modules/storage/versions.tf` is inspected
- **THEN** the `version` attribute for `source = "hashicorp/google"` reads `"~> 6.14.1"`

### REQ-PIN-008: monitoring Module versions.tf Pinned to Patch Level

**Previously**: `terraform/modules/monitoring/versions.tf` contained `version = "~> 6.0"`.

`terraform/modules/monitoring/versions.tf` **MUST** change the `hashicorp/google` provider constraint to `version = "~> 6.14.1"`.

#### Scenario: monitoring versions.tf uses ~> 6.14.1 · `code-based` · `critical`

- **WHEN** `terraform/modules/monitoring/versions.tf` is inspected
- **THEN** the `version` attribute for `source = "hashicorp/google"` reads `"~> 6.14.1"`

### REQ-PIN-009: All versions.tf Files Consistently Pinned

After the change, **no** `versions.tf` file in `terraform/` **MUST** contain `version = "~> 6.0"` for the `hashicorp/google` provider.

A recursive search for the literal `"~> 6.0"` in all `.tf` files under `terraform/` **MUST** return zero matches.

#### Scenario: No ~> 6.0 strings remain in any versions.tf · `code-based` · `critical`

- **WHEN** a recursive search for `~> 6.0` is performed across all `.tf` files in `terraform/`
- **THEN** no file contains the string `"~> 6.0"` in a `required_providers` block

#### Scenario: terraform validate passes in all roots after pin · `code-based` · `critical`

- **WHEN** `terraform validate` is run in `terraform/bootstrap/`, `terraform/environments/dev/`, and `terraform/environments/prod/` after running `terraform init -upgrade` in each
- **THEN** all three commands exit with code `0`

---

## Acceptance Criteria Summary

| Requirement ID | Type     | Priority | Scenarios |
|----------------|----------|----------|-----------|
| REQ-PIN-001    | MODIFIED | MUST     | 2         |
| REQ-PIN-002    | MODIFIED | MUST     | 1         |
| REQ-PIN-003    | MODIFIED | MUST     | 1         |
| REQ-PIN-004    | MODIFIED | MUST     | 1         |
| REQ-PIN-005    | MODIFIED | MUST     | 1         |
| REQ-PIN-006    | MODIFIED | MUST     | 1         |
| REQ-PIN-007    | MODIFIED | MUST     | 1         |
| REQ-PIN-008    | MODIFIED | MUST     | 1         |
| REQ-PIN-009    | MODIFIED | MUST     | 2         |

**Total Requirements**: 9
**Total Scenarios**: 11

---

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-PIN-001 › Bootstrap versions.tf uses ~> 6.14.1 | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-001 › Bootstrap versions.tf no longer contains ~> 6.0 | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-002 › cloud-run versions.tf uses ~> 6.14.1 | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-003 › iam versions.tf uses ~> 6.14.1 | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-004 › networking versions.tf uses ~> 6.14.1 | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-005 › spanner versions.tf uses ~> 6.14.1 | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-006 › pubsub versions.tf uses ~> 6.14.1 | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-007 › storage versions.tf uses ~> 6.14.1 | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-008 › monitoring versions.tf uses ~> 6.14.1 | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-009 › No ~> 6.0 strings remain in any versions.tf | code-based | critical | pass^3 = 1.00 |
| REQ-PIN-009 › terraform validate passes in all roots after pin | code-based | critical | pass^3 = 1.00 |
