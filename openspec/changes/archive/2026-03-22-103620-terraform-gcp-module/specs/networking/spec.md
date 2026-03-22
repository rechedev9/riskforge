# Delta Spec: Networking

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The networking module provisions a VPC, a /28 subnet for the VPC Access connector, and the connector itself. Cloud Run services use the connector to access VPC-internal resources (Spanner, etc.) while keeping egress scoped to private ranges.

## ADDED Requirements

### REQ-NET-001: VPC Creation

The networking module **MUST** create a `google_compute_network` with `auto_create_subnetworks = false` (custom-mode VPC).

#### Scenario: Custom-mode VPC created · `code-based` · `critical`

- **WHEN** `terraform/modules/networking/main.tf` is read
- **THEN** a `google_compute_network` resource exists with `auto_create_subnetworks = false`

---

### REQ-NET-002: VPC Access Connector Subnet

The networking module **MUST** create a `google_compute_subnetwork` with a `/28` CIDR range for the VPC Access connector. The subnet region **MUST** be parameterized via `var.region`.

#### Scenario: /28 subnet for connector created · `code-based` · `critical`

- **WHEN** `terraform/modules/networking/main.tf` is read
- **THEN** a `google_compute_subnetwork` resource exists with `ip_cidr_range` containing `"/28"`

#### Scenario: Subnet region parameterized · `code-based` · `critical`

- **WHEN** the `google_compute_subnetwork` resource is inspected
- **THEN** `region = var.region`

---

### REQ-NET-003: VPC Access Connector

The networking module **MUST** create a `google_vpc_access_connector` resource referencing the `/28` subnet. The connector **MUST** use `machine_type = "e2-micro"` as the default.

#### Scenario: VPC Access connector created · `code-based` · `critical`

- **WHEN** `terraform/modules/networking/main.tf` is read
- **THEN** a `google_vpc_access_connector` resource exists referencing the connector subnet

#### Scenario: Connector uses e2-micro · `code-based` · `standard`

- **WHEN** the `google_vpc_access_connector` resource is inspected
- **THEN** `machine_type = "e2-micro"`

---

### REQ-NET-004: Networking Module Outputs

The networking module **MUST** output `vpc_connector_id` and `vpc_id` for consumption by the cloud-run module.

#### Scenario: Networking outputs declared · `code-based` · `critical`

- **WHEN** `terraform/modules/networking/outputs.tf` is read
- **THEN** it declares `output "vpc_connector_id"` and `output "vpc_id"`

---

### REQ-NET-005: Networking Module Structure

The networking module **MUST** contain `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf`. The `versions.tf` **MUST** declare `hashicorp/google ~> 6.0`.

#### Scenario: Networking module files present · `code-based` · `critical`

- **WHEN** the contents of `terraform/modules/networking/` are listed
- **THEN** `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf` exist

---

### REQ-NET-006: VPC Access API Enablement

The networking module **SHOULD** enable the `vpcaccess.googleapis.com` API via `google_project_service` with `disable_on_destroy = false`.

#### Scenario: VPC Access API enabled · `code-based` · `standard`

- **WHEN** `terraform/modules/networking/main.tf` is read
- **THEN** a `google_project_service` resource exists with `service = "vpcaccess.googleapis.com"` and `disable_on_destroy = false`

---

## MODIFIED Requirements

(none)

## REMOVED Requirements

(none)

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-NET-001    | ADDED | MUST     | 1         |
| REQ-NET-002    | ADDED | MUST     | 2         |
| REQ-NET-003    | ADDED | MUST     | 2         |
| REQ-NET-004    | ADDED | MUST     | 1         |
| REQ-NET-005    | ADDED | MUST     | 1         |
| REQ-NET-006    | ADDED | SHOULD   | 1         |

**Total Requirements**: 6
**Total Scenarios**: 8

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-NET-001 > Custom-mode VPC created | code-based | critical | pass^3 = 1.00 |
| REQ-NET-002 > /28 subnet for connector created | code-based | critical | pass^3 = 1.00 |
| REQ-NET-002 > Subnet region parameterized | code-based | critical | pass^3 = 1.00 |
| REQ-NET-003 > VPC Access connector created | code-based | critical | pass^3 = 1.00 |
| REQ-NET-003 > Connector uses e2-micro | code-based | standard | pass@3 >= 0.90 |
| REQ-NET-004 > Networking outputs declared | code-based | critical | pass^3 = 1.00 |
| REQ-NET-005 > Networking module files present | code-based | critical | pass^3 = 1.00 |
| REQ-NET-006 > VPC Access API enabled | code-based | standard | pass@3 >= 0.90 |
