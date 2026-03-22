# Delta Spec: CI/CD Security

**Change**: owasp-security-hardening
**Date**: 2026-03-22T15:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

`.github/workflows/terraform.yml` has two security defects. H4: the `Post Plan to PR` step posts `${{ steps.plan.outputs.stdout }}` verbatim to PR comments — this output can contain Terraform-resolved secret values from provider configuration or data sources, making them visible to all repository readers. M2: the `Terraform Plan` step at line 55 carries `continue-on-error: true`, which masks plan failures and allows the workflow to proceed to the `Plan Status` gate step silently — a redundant and risky pattern that makes failure state ambiguous in the GitHub UI.

## MODIFIED Requirements

### REQ-CICD-001: Remove continue-on-error from Plan Step

**Previously**: `.github/workflows/terraform.yml` line 55 contained `continue-on-error: true` on the `Terraform Plan` step (id: `plan`).

The `Terraform Plan` step **MUST NOT** include `continue-on-error: true`. The step **MUST** fail immediately on a non-zero exit code.

The `Plan Status` step that follows **MAY** be retained for backward-compatible failure signaling, but the `continue-on-error` flag itself **MUST** be absent.

#### Scenario: Plan step has no continue-on-error flag · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is inspected
- **THEN** the step with `id: plan` does not contain a `continue-on-error` key at all

#### Scenario: Plan failure fails the job · `code-based` · `critical`

- **GIVEN** a branch where `terraform plan` would exit with code 1 (e.g., provider auth failure)
- **WHEN** the GitHub Actions plan job runs
- **THEN** the job step `Terraform Plan` is marked as `failed` in the GitHub UI, and the `Post Plan to PR` step is not reached (the subsequent `if: steps.plan.outcome == 'failure'` `Plan Status` step triggers exit 1)

### REQ-CICD-002: Mask Terraform Plan Output in PR Comment

**Previously**: The `Post Plan to PR` step used `${{ steps.plan.outputs.stdout }}` to post the complete, unfiltered plan output as a PR comment.

The `Post Plan to PR` step **MUST** replace the verbatim stdout reference with a filtered summary derived from running `terraform show -no-color tfplan | grep -E '^\s*(#|~|\+|-)' | head -100`.

The filtered output **MUST** be produced as a separate `run` step before the `Post Plan to PR` step and stored in a step output or environment variable. The PR comment **MUST** reference this filtered output, not `steps.plan.outputs.stdout`.

The PR comment **MUST NOT** contain any line that was not produced by the `grep -E '^\s*(#|~|\+|-)'` filter.

#### Scenario: PR comment step reads filtered output not raw stdout · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is inspected
- **THEN** the `Post Plan to PR` step does not reference `steps.plan.outputs.stdout` anywhere in its `script:` or `run:` block

#### Scenario: Filter step present before PR comment step · `code-based` · `critical`

- **WHEN** the workflow job steps for the `plan` job are inspected in order
- **THEN** a `run:` step executing `terraform show -no-color tfplan` piped through `grep -E '^\s*(#|~|\+|-)'` and `head -100` appears before the `Post Plan to PR` step

#### Scenario: Plan output limited to 100 lines · `code-based` · `standard`

- **WHEN** the filter command in the workflow is inspected
- **THEN** `head -100` is present in the pipeline to cap the PR comment body at 100 summary lines

### REQ-CICD-003: Workflow Passes terraform fmt Check

The `.github/workflows/terraform.yml` file changes **MUST NOT** affect the `terraform fmt -check -recursive terraform/` step that already runs in the workflow, as that step validates IaC formatting and is unrelated to workflow YAML changes.

The workflow YAML itself **SHOULD** remain valid GitHub Actions syntax and pass `actionlint` (if available in CI).

#### Scenario: Terraform fmt step still present and unchanged · `code-based` · `standard`

- **WHEN** `.github/workflows/terraform.yml` is inspected after the change
- **THEN** a step running `terraform fmt -check -recursive ../../` is still present in the `plan` job and runs before the `Terraform Plan` step

---

## Acceptance Criteria Summary

| Requirement ID | Type     | Priority | Scenarios |
|----------------|----------|----------|-----------|
| REQ-CICD-001   | MODIFIED | MUST     | 2         |
| REQ-CICD-002   | MODIFIED | MUST     | 3         |
| REQ-CICD-003   | ADDED    | SHOULD   | 1         |

**Total Requirements**: 3
**Total Scenarios**: 6

---

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-CICD-001 › Plan step has no continue-on-error flag | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-001 › Plan failure fails the job | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-002 › PR comment step reads filtered output not raw stdout | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-002 › Filter step present before PR comment step | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-002 › Plan output limited to 100 lines | code-based | standard | pass@3 ≥ 0.90 |
| REQ-CICD-003 › Terraform fmt step still present and unchanged | code-based | standard | pass@3 ≥ 0.90 |
