# Delta Spec: Firewall Rules

**Change**: owasp-security-hardening
**Date**: 2026-03-22T15:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

`terraform/modules/networking/main.tf` creates a VPC, subnet, and VPC connector but contains zero `google_compute_firewall` resources. The VPC has no ingress or egress controls whatsoever. This spec covers C2 (deny-all ingress default, allow health-check ingress, allow GCP private-API egress, deny-all other egress) and M1 (Cloud Armor placeholder policy with rate-limit rule). Additionally, L2 (VPC connector `machine_type` variable) is included as it belongs to the same networking module.

## ADDED Requirements

### REQ-FW-001: Default Deny-All Ingress Firewall Rule

`modules/networking/main.tf` **MUST** contain a `google_compute_firewall` resource named `"deny-all-ingress-${var.environment}"` with `direction = "INGRESS"`, `priority = 65534`, `deny { protocol = "all" }`, and `source_ranges = ["0.0.0.0/0"]`.

#### Scenario: Deny-all ingress rule present in plan · `code-based` · `critical`

- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output contains `module.networking.google_compute_firewall.deny_all_ingress` with `direction = "INGRESS"` and `priority = 65534`

#### Scenario: Deny rule uses deny block not allow block · `code-based` · `critical`

- **WHEN** the HCL for the deny-all ingress firewall resource is inspected
- **THEN** a `deny { protocol = "all" }` block is present and no `allow` block exists within the same resource

### REQ-FW-002: Allow Health-Check Ingress Firewall Rule

`modules/networking/main.tf` **MUST** contain a `google_compute_firewall` resource permitting ingress from GCP health-check source ranges `35.191.0.0/16` and `130.211.0.0/22` on TCP port `443`.

The rule **MUST** have `priority` lower than 65534 (i.e., <= 1000) so it takes precedence over the deny-all ingress rule.

#### Scenario: Health-check allow rule present · `code-based` · `critical`

- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output contains a `google_compute_firewall` resource with `source_ranges` including `"35.191.0.0/16"` and `"130.211.0.0/22"` and `direction = "INGRESS"`

#### Scenario: Health-check rule priority beats deny-all · `code-based` · `critical`

- **WHEN** the HCL for the health-check allow rule is inspected
- **THEN** the `priority` value is less than or equal to `1000`

### REQ-FW-003: Allow GCP Private-API Egress Rule

`modules/networking/main.tf` **MUST** contain a `google_compute_firewall` resource permitting egress to `199.36.153.8/30` (GCP restricted APIs VIP) on TCP ports `443`.

The rule **MUST** have `direction = "EGRESS"` and `priority` <= 1000.

#### Scenario: Private-API egress allow rule present · `code-based` · `critical`

- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output contains a `google_compute_firewall` resource with `destination_ranges` including `"199.36.153.8/30"` and `direction = "EGRESS"`

### REQ-FW-004: Default Deny-All Egress Firewall Rule

`modules/networking/main.tf` **MUST** contain a `google_compute_firewall` resource with `direction = "EGRESS"`, `priority = 65534`, `deny { protocol = "all" }`, and `destination_ranges = ["0.0.0.0/0"]`.

The allow-egress rule for private APIs (REQ-FW-003) **MUST** have a lower priority number than this deny-all egress rule so that GCP API egress is not blocked.

#### Scenario: Deny-all egress rule present in plan · `code-based` · `critical`

- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output contains `module.networking.google_compute_firewall.deny_all_egress` with `direction = "EGRESS"` and `priority = 65534`

#### Scenario: Egress allow precedes deny · `code-based` · `critical`

- **GIVEN** both the allow-private-API-egress rule and the deny-all-egress rule are present
- **WHEN** their `priority` values are compared
- **THEN** the allow-private-API-egress `priority` value is numerically lower than `65534`

### REQ-FW-005: Cloud Armor Security Policy Placeholder

`modules/networking/main.tf` **MUST** contain a `google_compute_security_policy` resource with at least one rule applying rate limiting of 1000 requests per minute per IP.

The resource **MUST** include a code comment stating that attachment to a load balancer frontend is required and references the `glb-cloud-armor` follow-up change.

The policy **MUST NOT** be attached to any load balancer resource in this change (no `google_compute_backend_service` is added).

#### Scenario: Security policy resource appears in plan · `code-based` · `standard`

- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/`
- **THEN** the plan output contains `module.networking.google_compute_security_policy` with a rate-limit rule threshold of `1000` per interval of `60` seconds

#### Scenario: Policy has no backend service attachment · `code-based` · `standard`

- **WHEN** the networking module HCL is searched for `security_policy` attribute references
- **THEN** no `google_compute_backend_service` resource exists in `modules/networking/main.tf`

### REQ-FW-006: VPC Connector machine_type Variable

`modules/networking/main.tf` **MUST** reference `var.connector_machine_type` for the `machine_type` attribute of `google_vpc_access_connector.connector` instead of the hardcoded `"e2-micro"` string.

**Previously**: `machine_type = "e2-micro"` was hardcoded at line 48 of `modules/networking/main.tf`.

`modules/networking/variables.tf` **MUST** declare a `connector_machine_type` variable of type `string` with `default = "e2-micro"` and a description.

`terraform/environments/prod/terraform.tfvars` **MUST** set `connector_machine_type = "e2-standard-4"`.

#### Scenario: machine_type uses variable in module · `code-based` · `standard`

- **WHEN** `modules/networking/main.tf` is inspected
- **THEN** the `machine_type` attribute on `google_vpc_access_connector.connector` reads `machine_type = var.connector_machine_type` and no literal `"e2-micro"` remains in that attribute

#### Scenario: Prod tfvars overrides to e2-standard-4 · `code-based` · `standard`

- **WHEN** `terraform/environments/prod/terraform.tfvars` is inspected
- **THEN** it contains the line `connector_machine_type = "e2-standard-4"`

---

## MODIFIED Requirements

### REQ-FW-007: VPC Connector machine_type Hardcoded Value Replaced

**Previously**: `terraform/modules/networking/main.tf` line 48 contained `machine_type = "e2-micro"` as a literal string with no variable indirection.

The networking module **MUST** now reference `var.connector_machine_type` instead of the literal string `"e2-micro"`, enabling per-environment override without module modification.

#### Scenario: No hardcoded e2-micro string in connector resource · `code-based` · `standard`

- **WHEN** the `google_vpc_access_connector.connector` resource block is inspected
- **THEN** the string `"e2-micro"` does not appear as a literal value for the `machine_type` attribute

---

## Acceptance Criteria Summary

| Requirement ID | Type     | Priority | Scenarios |
|----------------|----------|----------|-----------|
| REQ-FW-001     | ADDED    | MUST     | 2         |
| REQ-FW-002     | ADDED    | MUST     | 2         |
| REQ-FW-003     | ADDED    | MUST     | 1         |
| REQ-FW-004     | ADDED    | MUST     | 2         |
| REQ-FW-005     | ADDED    | SHOULD   | 2         |
| REQ-FW-006     | ADDED    | MUST     | 2         |
| REQ-FW-007     | MODIFIED | MUST     | 1         |

**Total Requirements**: 7
**Total Scenarios**: 12

---

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-FW-001 › Deny-all ingress rule present in plan | code-based | critical | pass^3 = 1.00 |
| REQ-FW-001 › Deny rule uses deny block not allow block | code-based | critical | pass^3 = 1.00 |
| REQ-FW-002 › Health-check allow rule present | code-based | critical | pass^3 = 1.00 |
| REQ-FW-002 › Health-check rule priority beats deny-all | code-based | critical | pass^3 = 1.00 |
| REQ-FW-003 › Private-API egress allow rule present | code-based | critical | pass^3 = 1.00 |
| REQ-FW-004 › Deny-all egress rule present in plan | code-based | critical | pass^3 = 1.00 |
| REQ-FW-004 › Egress allow precedes deny | code-based | critical | pass^3 = 1.00 |
| REQ-FW-005 › Security policy resource appears in plan | code-based | standard | pass@3 ≥ 0.90 |
| REQ-FW-005 › Policy has no backend service attachment | code-based | standard | pass@3 ≥ 0.90 |
| REQ-FW-006 › machine_type uses variable in module | code-based | standard | pass@3 ≥ 0.90 |
| REQ-FW-006 › Prod tfvars overrides to e2-standard-4 | code-based | standard | pass@3 ≥ 0.90 |
| REQ-FW-007 › No hardcoded e2-micro string in connector resource | code-based | standard | pass@3 ≥ 0.90 |
