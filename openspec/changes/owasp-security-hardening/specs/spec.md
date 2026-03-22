# Delta Spec Index: OWASP Security Hardening

**Change**: owasp-security-hardening
**Date**: 2026-03-22T15:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Overview

This index lists all domain spec files for the `owasp-security-hardening` change. The change addresses 15 OWASP-mapped security findings across the Terraform IaC for the GCP appetite matching engine. All changes are Terraform-only — no Go application code is modified.

Seven spec domains cover the full proposal scope:

| Domain | File | Proposal Items | Requirements | Scenarios |
|--------|------|----------------|--------------|-----------|
| [audit-logging](./audit-logging/spec.md) | `modules/iam/main.tf` | C3 | 3 | 5 |
| [firewall-rules](./firewall-rules/spec.md) | `modules/networking/main.tf` | C2, M1, L2 | 7 | 12 |
| [iam-scoping](./iam-scoping/spec.md) | `modules/iam/main.tf`, `bootstrap/main.tf` | H2, H3 | 3 | 8 |
| [ci-cd-security](./ci-cd-security/spec.md) | `.github/workflows/terraform.yml` | H4, M2 | 3 | 6 |
| [storage-security](./storage-security/spec.md) | `modules/storage/main.tf`, `bootstrap/main.tf` | M3, H5, L1 | 5 | 8 |
| [monitoring-worker](./monitoring-worker/spec.md) | `modules/monitoring/`, `environments/*/main.tf` | M4 | 5 | 8 |
| [provider-pinning](./provider-pinning/spec.md) | All `versions.tf` files | M5 | 9 | 11 |

**Total Domains**: 7
**Total Requirements**: 35
**Total Scenarios**: 58

---

## Proposal Coverage

| Proposal Item | Domain | Status |
|---------------|--------|--------|
| C1 — Keep allow_unauthenticated, add TODO comment | *(not specced — no Terraform resource change; covered by code comment only)* | out-of-scope for spec |
| C2 — Firewall rules | firewall-rules | covered |
| C3 — Audit logging | audit-logging | covered |
| H1 — KMS module (CMEK opt-in) | *(not specced in this batch — KMS module is a new module; spec deferred to design phase per proposal note)* | deferred |
| H2 — Per-secret IAM scoping | iam-scoping | covered |
| H3 — Terraform SA minimum roles | iam-scoping | covered |
| H4 — Plan output masking | ci-cd-security | covered |
| H5 — Artifact Registry IAM | storage-security | covered |
| M1 — Cloud Armor placeholder | firewall-rules | covered |
| M2 — Remove continue-on-error | ci-cd-security | covered |
| M3 — State bucket public_access_prevention | storage-security | covered |
| M4 — Worker monitoring | monitoring-worker | covered |
| M5 — Provider version pinning | provider-pinning | covered |
| L1 — AR vulnerability scanning | storage-security | covered |
| L2 — VPC connector machine_type variable | firewall-rules | covered |

---

## Out-of-Scope Confirmation

The following items from the proposal's Out-of-Scope section are **not** covered by any spec:

- Replacing `allow_unauthenticated = true` with real auth — deferred to follow-on change
- Attaching Cloud Armor to a load balancer — deferred to `glb-cloud-armor` change
- Creating `google_secret_manager_secret` resources — no secrets in IaC yet
- Any Go application code changes — IaC-only
- CMEK enabled by default — module added but flag defaults to `false`; H1 KMS module spec deferred

---

## Eval Definitions Summary

| Domain | code-based | model-based | human-based | critical | standard |
|--------|-----------|-------------|-------------|----------|----------|
| audit-logging | 5 | 0 | 0 | 5 | 0 |
| firewall-rules | 12 | 0 | 0 | 7 | 5 |
| iam-scoping | 8 | 0 | 0 | 8 | 0 |
| ci-cd-security | 6 | 0 | 0 | 4 | 2 |
| storage-security | 8 | 0 | 0 | 5 | 3 |
| monitoring-worker | 8 | 0 | 0 | 6 | 2 |
| provider-pinning | 11 | 0 | 0 | 11 | 0 |
| **Total** | **58** | **0** | **0** | **46** | **12** |

---

## Warnings

- **H1 (KMS module)** is in-scope per the proposal but has no spec file here. The KMS module is a new module addition with `enable_cmek = false` by default; its spec can be authored independently when the design phase defines the module interface. No existing resources are affected by its absence in this batch.
- **C1 (allow_unauthenticated TODO comment)** requires only a code comment change, not a Terraform resource change. No testable Terraform scenario exists for a comment; verification is human-based and can be confirmed during code review.
- The `REQ-IAM-003` scenario for plan showing destroy of secretAccessor bindings assumes the bindings exist in state. In a fresh environment (no prior apply), this scenario applies as "not present in plan" — the test should be adapted to the actual state.
