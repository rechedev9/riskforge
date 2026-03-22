# Proposal: OWASP Security Hardening

**Change ID**: owasp-security-hardening
**Date**: 2026-03-22T14:30:00Z
**Status**: draft

---

## Intent

The Terraform infrastructure for the appetite matching engine has 15 security findings across critical, high, medium, and low severities — including a fully public API in prod, zero firewall rules, no audit logging, and Terraform plan output leaking potential secrets to public PR comments. This change hardens the IaC to address all OWASP-mapped findings without touching any Go application code.

## Scope

### In Scope

- C1: Keep `allow_unauthenticated = true` in both environments for now; add an explicit `# TODO: disable when app-level auth middleware is built` comment and document that removing `allUsers` is a prerequisite for production readiness. No Cloud Endpoints or API Gateway is added — that is an app-level concern.
- C2: Add `google_compute_firewall` rules to `modules/networking/main.tf` — deny-all ingress default, allow ingress from GCP health-check ranges, allow egress to GCP private APIs (`199.36.153.8/30`), deny all other egress.
- C3: Add `google_project_iam_audit_config` for `allServices` (`DATA_READ`, `DATA_WRITE`, `ADMIN_READ`) in `modules/iam/main.tf` (or a new `audit.tf` alongside it).
- H1: Add an optional `terraform/modules/kms/` module with `google_kms_key_ring` and `google_kms_crypto_key` resources. Wire CMEK into GCS (`encryption.default_kms_key_name`), Spanner (`encryption_config`), and Artifact Registry (`kms_key_name`) via a boolean `enable_cmek` variable defaulting to `false`. Not enabled in dev or prod by default.
- H2: Remove `roles/secretmanager.secretAccessor` from the project-scoped `sa_project_roles` list in `modules/iam/main.tf`. Replace with a `google_secret_manager_secret_iam_member` variable-driven pattern so per-secret bindings can be added when secrets are defined.
- H3: Add `google_project_iam_member` resources in `terraform/bootstrap/main.tf` granting the Terraform SA the minimum set of admin roles it requires (enumerated explicitly — no `roles/editor`).
- H4: Replace the verbatim `steps.plan.outputs.stdout` post to PR with `terraform show -no-color tfplan | grep -E '^\s*(#|~|+|-)' | head -100` (summary lines only). Remove `continue-on-error: true` from the Plan step.
- H5: Add `google_artifact_registry_repository_iam_member` resources in `modules/storage/main.tf` granting `roles/artifactregistry.writer` to the API and Worker SAs, and `roles/artifactregistry.reader` to the Cloud Run runtime SA.
- M1: Add a Cloud Armor `google_compute_security_policy` resource with a rate-limiting rule (1000 req/min per IP) as a placeholder in `modules/networking/main.tf`. The policy resource is created but not yet attached to a load balancer (no GLB is added in this change). A comment will note that attachment requires a GLB frontend — tracked as a follow-up.
- M2: Remove `continue-on-error: true` from the Terraform Plan step in `.github/workflows/terraform.yml` (also addressed as part of H4).
- M3: Add `public_access_prevention = "enforced"` to `google_storage_bucket.terraform_state` in `terraform/bootstrap/main.tf`.
- M4: Extend `modules/monitoring/` to accept a list of services (or a `worker_service_name` + `worker_service_url` pair) so the Worker can be monitored. Wire the Worker monitoring in both `environments/dev/main.tf` and `environments/prod/main.tf`.
- M5: Pin all `versions.tf` files (bootstrap + 7 modules) to `version = "~> 6.14.1"` (patch-level pessimistic constraint) based on the current `~> 6.0` baseline.
- L1: Add `vulnerability_scanning_config { enablement_config = "INHERITED" }` (or equivalent for provider 6.x) to `google_artifact_registry_repository` in `modules/storage/main.tf`.
- L2: Make VPC connector `machine_type` a variable with `"e2-micro"` as default and `"e2-standard-4"` recommended for prod. Update `terraform/environments/prod/terraform.tfvars` accordingly.

### Out of Scope

- Replacing `allow_unauthenticated = true` with real auth (Cloud Run IAP, Cloud Endpoints, API Gateway) — requires the Go application and a product decision on the auth model; tracked as a follow-on change.
- Attaching Cloud Armor to a load balancer — requires a Global External Application Load Balancer, which is a major infrastructure addition (NEG, backend service, URL map, HTTPS proxy, forwarding rule). Deferred to a dedicated `glb-cloud-armor` change.
- Creating Secret Manager secrets in Terraform — no secrets are defined in IaC yet; per-secret IAM bindings will be co-located with `google_secret_manager_secret` resources when those are added.
- Any changes to Go application code (`cmd/`, `internal/`) — this is an IaC-only change.
- CMEK enabled by default — key management policies and rotation schedules require additional planning. The module is added but the flag defaults to `false`.
- Cross-project KMS or external KMS — out of scope; single-project key rings only.

## Approach

All 15 findings map to existing files; no new modules are required except the optional `modules/kms/`. Each fix is independent and can be reviewed in isolation. The implementation proceeds from lowest-blast-radius to highest: bootstrap changes first (isolated root), then module changes (no environment impact until re-applied), then CI/CD workflow changes (no infrastructure impact), then environment wiring (triggers `terraform apply` on next push).

The only structural addition is `terraform/modules/kms/` which is gated behind `enable_cmek = false` so it has zero production impact unless deliberately enabled. Cloud Armor is added as a resource-only placeholder (no attachment) to establish the policy without requiring a GLB; the comment in code documents the attachment path.

### Key Decisions

| Decision | Choice | Rationale |
|---|---|---|
| C1: keep allow_unauthenticated | Retain `true`, add TODO comment | No Go app exists yet; removing `allUsers` would break the service with no replacement. The fix is documented and tracked. |
| H1: CMEK opt-in | Add module, default `enable_cmek = false` | CMEK requires KMS key grants to GCP service agents which is error-prone to get right; opt-in avoids breaking existing state on apply. |
| H3: Terraform SA roles | Enumerate minimal admin roles in bootstrap | `roles/editor` is overly broad; explicit admin roles per service are auditable and align with least-privilege. |
| M1: Cloud Armor placeholder | Create policy, no GLB attachment | Establishes the resource and rate-limit rule without the GLB dependency. Attaching requires a separate `glb-cloud-armor` change. |
| M4: Worker monitoring | Add `worker_service_name` / `worker_service_url` inputs to monitoring module | Avoids a breaking variable rename; backward compatible with current callers that only wire the API. |
| M5: Provider pinning | `~> 6.14.1` (patch-level) | Locks patch while allowing Terraform to download patch updates; prevents silent minor-version drift without requiring manual lockfile updates for every patch. |
| H4: Plan output masking | Print only add/change/destroy summary lines via `grep` filter, `head -100` cap | Simple, no external tooling required; removes the risk of secrets appearing in verbatim stdout. |

## Affected Areas

| Module / Area | File Path | Change Type | Risk Level |
|---|---|---|---|
| Cloud Run module | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/main.tf` | modify | low |
| Cloud Run versions | `/home/reche/projects/ProyectoAgentero/terraform/modules/cloud-run/versions.tf` | modify | low |
| Networking module | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/main.tf` | modify | medium |
| Networking versions | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/versions.tf` | modify | low |
| Networking variables | `/home/reche/projects/ProyectoAgentero/terraform/modules/networking/variables.tf` | modify | low |
| IAM module | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/main.tf` | modify | high |
| IAM versions | `/home/reche/projects/ProyectoAgentero/terraform/modules/iam/versions.tf` | modify | low |
| Storage module | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/main.tf` | modify | low |
| Storage versions | `/home/reche/projects/ProyectoAgentero/terraform/modules/storage/versions.tf` | modify | low |
| Monitoring module main | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/main.tf` | modify | low |
| Monitoring module variables | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/variables.tf` | modify | low |
| Monitoring versions | `/home/reche/projects/ProyectoAgentero/terraform/modules/monitoring/versions.tf` | modify | low |
| Spanner versions | `/home/reche/projects/ProyectoAgentero/terraform/modules/spanner/versions.tf` | modify | low |
| Pub/Sub versions | `/home/reche/projects/ProyectoAgentero/terraform/modules/pubsub/versions.tf` | modify | low |
| Bootstrap main | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/main.tf` | modify | medium |
| Bootstrap versions | `/home/reche/projects/ProyectoAgentero/terraform/bootstrap/versions.tf` | modify | low |
| Dev environment | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/main.tf` | modify | low |
| Dev tfvars | `/home/reche/projects/ProyectoAgentero/terraform/environments/dev/terraform.tfvars` | modify | low |
| Prod environment | `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/main.tf` | modify | low |
| Prod tfvars | `/home/reche/projects/ProyectoAgentero/terraform/environments/prod/terraform.tfvars` | modify | low |
| CI/CD workflow | `/home/reche/projects/ProyectoAgentero/.github/workflows/terraform.yml` | modify | medium |
| KMS module main (new) | `/home/reche/projects/ProyectoAgentero/terraform/modules/kms/main.tf` | create | low |
| KMS module variables (new) | `/home/reche/projects/ProyectoAgentero/terraform/modules/kms/variables.tf` | create | low |
| KMS module outputs (new) | `/home/reche/projects/ProyectoAgentero/terraform/modules/kms/outputs.tf` | create | low |
| KMS module versions (new) | `/home/reche/projects/ProyectoAgentero/terraform/modules/kms/versions.tf` | create | low |

**Total files affected**: 25
**New files**: 4 (kms module)
**Modified files**: 21
**Deleted files**: 0

## Risks

| Risk | Probability | Impact | Mitigation |
|---|---|---|---|
| H2: Removing project-scoped secretAccessor breaks runtime secret access | medium | high | Both API and Worker SAs currently read secrets via `secret_env_vars`. Removing the project-scoped role without adding per-secret bindings will cause Cloud Run startup failures. Mitigation: because no `google_secret_manager_secret` resources exist in IaC yet, the project-scoped removal only takes effect if secrets are manually provisioned. Add a guard comment; the per-secret binding pattern is established for when secrets are added. |
| H3: Incorrect Terraform SA role enumeration blocks CI/CD apply | medium | high | If the enumerated roles are too narrow, `terraform apply` will fail mid-run. Mitigation: derive the role list from current managed resource types (Run, Spanner, Storage, AR, IAM, VPC, Monitoring, KMS, Secret Manager, Pub/Sub) and validate against GCP documentation before applying. |
| M4: Monitoring module variable change breaks existing callers | low | medium | Adding `worker_service_name` / `worker_service_url` as optional variables (with `default = ""`) is backward compatible. If made required, both environment `main.tf` files must be updated in the same commit. |
| Firewall rules block Cloud Run egress | low | high | If egress rules are too restrictive, Cloud Run cannot reach Spanner/Pub/Sub. Mitigation: include `allow` rule for `199.36.153.8/30` (GCP restricted APIs) and `35.199.192.0/19` (health checks) before deny-all. Test in dev before prod. |
| Provider version pin causes lockfile conflict | low | low | Pinning to `~> 6.14.1` may conflict with the existing `.terraform.lock.hcl`. Mitigation: run `terraform init -upgrade` in each root after the change, commit updated lockfiles. |
| Cloud Armor placeholder resource incurs cost | low | low | A `google_compute_security_policy` resource alone has no cost unless attached. No billing risk until the GLB follow-up. |

**Overall Risk Level**: medium

The high-impact risks (H2 and H3) are both mitigated by the fact that no runtime secrets exist in IaC yet and that CI/CD can be tested in dev before prod. The firewall risk is real but addressable through careful rule ordering and dev-first testing.

## Rollback Plan

All changes are Terraform IaC. Rollback is a `git revert` followed by `terraform apply`.

### Steps to Rollback

1. Revert the commit: `git revert <commit-sha>` — produces a new commit that undoes all file changes.
2. Push the revert commit to main: CI/CD will trigger `terraform plan` and `terraform apply` on the reverted state.
3. For bootstrap changes (M3, H3): `cd terraform/bootstrap && terraform init && terraform apply` — bootstrap is a separate root not touched by the CI/CD pipeline; must be applied manually.
4. Verify Terraform state converges: check that `terraform plan` shows "No changes" after apply.
5. If a partial apply occurred (e.g., firewall rules created but monitoring failed): run `terraform apply` again on the reverted code to reconcile; Terraform is idempotent.

### Rollback Verification

- `terraform plan` shows "No changes. Your infrastructure matches the configuration." in both dev and prod environments.
- Bootstrap `terraform plan` shows "No changes."
- GitHub Actions plan jobs pass on the revert commit.
- GCP Console: verify `google_compute_firewall` resources and `google_project_iam_audit_config` are absent (or match pre-change state).

## Dependencies

### Internal Dependencies

- Bootstrap must be applied before environment modules (it creates the state bucket and Terraform SA). Bootstrap changes (M3, H3) should be applied first, before the environment PR is merged.
- KMS module must be initialized before `enable_cmek = true` is set in any environment. Since `enable_cmek` defaults to `false`, no ordering constraint exists for this change.

### External Dependencies

| Package | Version | Purpose | Already Installed |
|---|---|---|---|
| hashicorp/google provider | ~> 6.14.1 | All GCP resource management | yes (currently ~> 6.0) |

### Infrastructure Dependencies

- Database migration needed: no
- New environment variables: none (all new inputs have defaults)
- New GCP services enabled: `cloudkms.googleapis.com` (only if `enable_cmek = true` is set in future); `cloudasset.googleapis.com` may be needed for audit log export (optional, not required for `google_project_iam_audit_config`)
- New Terraform roots: none (bootstrap is existing; kms module is called from existing environment roots)

## Success Criteria

All of the following must be true for this change to be considered complete:

- [ ] `terraform validate` passes in `terraform/bootstrap/`, `terraform/environments/dev/`, and `terraform/environments/prod/`
- [ ] `terraform fmt -check -recursive terraform/` passes with zero formatting errors
- [ ] `terraform plan` in dev shows no unexpected destroy operations on existing resources (only adds/updates)
- [ ] `google_project_iam_audit_config` resource is present in plan output covering `allServices` with `DATA_READ`, `DATA_WRITE`, `ADMIN_READ`
- [ ] `google_compute_firewall` resources appear in plan — deny-all ingress default, allow health-check ingress, allow private-API egress
- [ ] `google_storage_bucket.terraform_state` plan shows `public_access_prevention = "enforced"`
- [ ] `.github/workflows/terraform.yml` plan step has no `continue-on-error: true` and PR comment does not use `steps.plan.outputs.stdout` verbatim
- [ ] All `versions.tf` files use `~> 6.14.1` (not `~> 6.0`)
- [ ] Monitoring module accepts `worker_service_name` and `worker_service_url`; both dev and prod environments wire the Worker to monitoring
- [ ] `google_artifact_registry_repository_iam_member` resources exist for API SA (writer) and Worker SA (writer) and Cloud Run runtime SA (reader)
- [ ] `google_project_iam_member` resources for Terraform SA are present in bootstrap with explicitly enumerated roles
- [ ] `roles/secretmanager.secretAccessor` is removed from the project-scoped loop in `modules/iam/main.tf`
- [ ] KMS module exists with `enable_cmek` variable defaulting to `false`; neither dev nor prod sets `enable_cmek = true`
- [ ] Cloud Armor `google_compute_security_policy` resource exists in networking module with rate-limit rule; code comment documents GLB attachment requirement
- [ ] VPC connector `machine_type` is a variable; prod `terraform.tfvars` sets it to `"e2-standard-4"`
- [ ] Rollback plan tested: `git revert` followed by `terraform plan` shows a clean convergence

## Open Questions

- Which exact roles should the Terraform SA hold? A proposed minimal set: `roles/run.admin`, `roles/spanner.admin`, `roles/storage.admin`, `roles/artifactregistry.admin`, `roles/iam.serviceAccountAdmin`, `roles/iam.workloadIdentityPoolAdmin`, `roles/compute.networkAdmin`, `roles/monitoring.admin`, `roles/cloudkms.admin`, `roles/secretmanager.admin`, `roles/pubsub.admin`, `roles/logging.admin`. This list should be confirmed against the principle of least privilege before the design phase writes the resource blocks.
- For H2, should the per-secret IAM binding pattern live in `modules/iam/` (accepting a `secret_bindings` map) or be co-located with each module that creates secrets? This affects the design of `modules/iam/variables.tf`. Given no secrets are in IaC yet, a comment and `secret_bindings = {}` variable stub is sufficient for this change; the pattern choice can be deferred.

---

**Next Step**: Review and approve this proposal, then run `sdd-spec` and `sdd-design` (can run in parallel).
