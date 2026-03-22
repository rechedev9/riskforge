# Delta Spec: CI/CD

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The CI/CD domain covers the GitHub Actions workflow at `.github/workflows/terraform.yml`. The workflow uses Workload Identity Federation for keyless authentication, runs `terraform plan` on pull requests (with PR comment output), and runs `terraform apply` on merge to `main`. A matrix strategy handles dev and prod environments with prod requiring approval.

## ADDED Requirements

### REQ-CICD-001: Workflow Trigger Configuration

The GitHub Actions workflow **MUST** trigger on `pull_request` and `push` to the `main` branch. The triggers **MUST** be scoped to paths under `terraform/` and `.github/workflows/terraform.yml` to avoid unnecessary runs.

#### Scenario: Workflow triggers on terraform path changes · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is read
- **THEN** the `on` block includes `pull_request.paths` and `push.paths` filters containing `"terraform/**"` and `".github/workflows/terraform.yml"`

#### Scenario: Push trigger scoped to main branch · `code-based` · `critical`

- **WHEN** the `on.push` block is inspected
- **THEN** `branches` includes `"main"`

---

### REQ-CICD-002: Workload Identity Federation Authentication

The workflow **MUST** use the `google-github-actions/auth` action with `workload_identity_provider` and `service_account` inputs. The workflow **MUST NOT** use `credentials_json` or any stored service account key. The `id-token: write` permission **MUST** be declared for OIDC token generation.

#### Scenario: WIF auth action used · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is read
- **THEN** a step uses `google-github-actions/auth@*` with `workload_identity_provider` input

#### Scenario: No stored credentials · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is searched for `credentials_json`
- **THEN** no match is found

#### Scenario: OIDC token permission declared · `code-based` · `critical`

- **WHEN** the `permissions` block is inspected
- **THEN** `id-token: write` is set

---

### REQ-CICD-003: Matrix Strategy for Environments

The workflow **MUST** use a matrix strategy to run against both `dev` and `prod` environments. The working directory for each matrix entry **MUST** be `terraform/environments/${{ matrix.environment }}`.

#### Scenario: Matrix includes dev and prod · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is read
- **THEN** `strategy.matrix.environment` contains `["dev", "prod"]` (or equivalent)

#### Scenario: Working directory uses matrix variable · `code-based` · `critical`

- **WHEN** the terraform steps are inspected
- **THEN** the working directory references `${{ matrix.environment }}` in a path like `terraform/environments/${{ matrix.environment }}`

---

### REQ-CICD-004: Plan on Pull Request

The workflow **MUST** run `terraform plan` on pull request events. The plan output **SHOULD** be posted as a PR comment for review. The workflow **MUST** run `terraform init` before `terraform plan`.

#### Scenario: Plan runs on PR · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is read
- **THEN** a step runs `terraform plan` conditionally on `github.event_name == 'pull_request'` (or equivalent conditional logic)

#### Scenario: Init runs before plan · `code-based` · `critical`

- **WHEN** the workflow steps are inspected in order
- **THEN** a `terraform init` step precedes the `terraform plan` step

---

### REQ-CICD-005: Apply on Merge to Main

The workflow **MUST** run `terraform apply -auto-approve` only on push events to `main` (i.e., after merge). The workflow **MUST NOT** run apply on pull request events.

#### Scenario: Apply runs only on push to main · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is read
- **THEN** the `terraform apply` step is conditioned on `github.event_name == 'push'` (or equivalent)

#### Scenario: Apply uses -auto-approve · `code-based` · `critical`

- **WHEN** the apply step is inspected
- **THEN** the command includes `-auto-approve`

---

### REQ-CICD-006: Prod Environment Protection

The prod environment **SHOULD** use GitHub environment protection rules requiring manual approval before `terraform apply`. This is configured in the GitHub repository settings, but the workflow **MUST** reference the environment name to enable it.

#### Scenario: Workflow references environment for protection · `code-based` · `standard`

- **WHEN** `.github/workflows/terraform.yml` is read
- **THEN** the job or step references an `environment` field using the matrix variable (e.g., `environment: ${{ matrix.environment }}`)

---

### REQ-CICD-007: Terraform Setup

The workflow **MUST** use `hashicorp/setup-terraform` action to install Terraform. The version **SHOULD** be pinned or constrained.

#### Scenario: Terraform setup action used · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is read
- **THEN** a step uses `hashicorp/setup-terraform@*`

---

### REQ-CICD-008: Terraform Format and Validate

The workflow **SHOULD** run `terraform fmt -check` and `terraform validate` as early steps to catch formatting and syntax issues before planning.

#### Scenario: Format check runs · `code-based` · `standard`

- **WHEN** `.github/workflows/terraform.yml` is read
- **THEN** a step runs `terraform fmt -check` or `terraform fmt -check -recursive`

#### Scenario: Validate runs · `code-based` · `standard`

- **WHEN** `.github/workflows/terraform.yml` is read
- **THEN** a step runs `terraform validate`

---

### REQ-CICD-009: No Hardcoded Credentials in Workflow

The workflow **MUST NOT** contain hardcoded GCP project IDs, service account emails, or WIF provider names. These values **MUST** be referenced via GitHub Actions secrets or variables (e.g., `${{ secrets.WIF_PROVIDER }}`, `${{ vars.PROJECT_ID }}`).

#### Scenario: No hardcoded project ID in workflow · `code-based` · `critical`

- **WHEN** `.github/workflows/terraform.yml` is searched for literal GCP project ID patterns
- **THEN** no hardcoded values are found; all sensitive values reference `${{ secrets.* }}` or `${{ vars.* }}`

---

## MODIFIED Requirements

(none)

## REMOVED Requirements

(none)

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-CICD-001   | ADDED | MUST     | 2         |
| REQ-CICD-002   | ADDED | MUST     | 3         |
| REQ-CICD-003   | ADDED | MUST     | 2         |
| REQ-CICD-004   | ADDED | MUST     | 2         |
| REQ-CICD-005   | ADDED | MUST     | 2         |
| REQ-CICD-006   | ADDED | SHOULD   | 1         |
| REQ-CICD-007   | ADDED | MUST     | 1         |
| REQ-CICD-008   | ADDED | SHOULD   | 2         |
| REQ-CICD-009   | ADDED | MUST     | 1         |

**Total Requirements**: 9
**Total Scenarios**: 16

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-CICD-001 > Workflow triggers on terraform path changes | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-001 > Push trigger scoped to main branch | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-002 > WIF auth action used | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-002 > No stored credentials | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-002 > OIDC token permission declared | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-003 > Matrix includes dev and prod | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-003 > Working directory uses matrix variable | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-004 > Plan runs on PR | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-004 > Init runs before plan | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-005 > Apply runs only on push to main | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-005 > Apply uses -auto-approve | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-006 > Workflow references environment for protection | code-based | standard | pass@3 >= 0.90 |
| REQ-CICD-007 > Terraform setup action used | code-based | critical | pass^3 = 1.00 |
| REQ-CICD-008 > Format check runs | code-based | standard | pass@3 >= 0.90 |
| REQ-CICD-008 > Validate runs | code-based | standard | pass@3 >= 0.90 |
| REQ-CICD-009 > No hardcoded project ID in workflow | code-based | critical | pass^3 = 1.00 |
