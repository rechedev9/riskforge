# SDD Spec Summary: terraform-gcp-module

**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Overview

This document is the consolidated spec summary for the `terraform-gcp-module` change. Full domain specs are in `openspec/changes/terraform-gcp-module/specs/{domain}/spec.md`.

All requirements are **ADDED** -- no existing Terraform infrastructure exists in the repository. No existing specs exist in `openspec/specs/`.

## Domains

1. **bootstrap** -- State bucket, WIF pool/provider, Terraform SA (6 requirements, 12 scenarios)
2. **iam** -- Service accounts, least-privilege bindings (5 requirements, 8 scenarios)
3. **networking** -- VPC, /28 subnet, VPC Access connector (6 requirements, 8 scenarios)
4. **spanner** -- Instance, database with DDL, database IAM (6 requirements, 9 scenarios)
5. **cloud-run** -- Generic v2 service, dynamic env vars, resource IAM (10 requirements, 13 scenarios)
6. **pubsub** -- Topics, push/pull subscriptions, DLQ, IAM (7 requirements, 11 scenarios)
7. **storage** -- Document bucket, Artifact Registry, lifecycle rules (6 requirements, 10 scenarios)
8. **monitoring** -- Alert policies, uptime checks, notification channels (8 requirements, 11 scenarios)
9. **environments** -- Dev/prod wiring, parameterization, state isolation (10 requirements, 22 scenarios)
10. **ci-cd** -- GitHub Actions workflow, WIF auth, plan/apply pipeline (9 requirements, 16 scenarios)

## Cross-Domain Consistency Check

- **Requirement ID collisions**: None. Each domain uses a unique prefix (BOOT, IAM, NET, SPAN, CRUN, PSUB, STOR, MON, ENV, CICD).
- **Contradictions**: None found.
- **Missing coverage**: All proposal in-scope items have corresponding requirements:
  - 7 modules: bootstrap, iam, networking, spanner, cloud-run, pubsub, storage, monitoring -- all covered
  - 2 environments: dev, prod -- covered in REQ-ENV-*
  - Artifact Registry: covered in REQ-STOR-003
  - Initial Spanner DDL: covered in REQ-SPAN-002
  - CI/CD workflow: covered in REQ-CICD-*
  - Secret Manager placeholders: covered implicitly via REQ-CRUN-004 (secret_env_vars dynamic block)
  - WIF auth: covered in REQ-BOOT-002, REQ-BOOT-003, REQ-CICD-002
- **Scope creep**: None. No specs cover out-of-scope items (project creation, custom domains, app deployment, multi-region, VPN, migration tooling, cost optimization).

## Totals

| Domain       | MUST | SHOULD | MAY | Scenarios |
|-------------|------|--------|-----|-----------|
| bootstrap   | 6    | 0      | 0   | 12        |
| iam         | 5    | 0      | 0   | 8         |
| networking  | 5    | 1      | 0   | 8         |
| spanner     | 6    | 0      | 0   | 9         |
| cloud-run   | 10   | 0      | 0   | 13        |
| pubsub      | 7    | 0      | 0   | 11        |
| storage     | 5    | 1      | 0   | 10        |
| monitoring  | 8    | 0      | 0   | 11        |
| environments| 10   | 0      | 0   | 22        |
| ci-cd       | 7    | 2      | 0   | 16        |
| **TOTAL**   | **69** | **4** | **0** | **120** |
