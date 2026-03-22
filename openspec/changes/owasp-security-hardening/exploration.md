# Exploration: OWASP Security Hardening

**Date**: 2026-03-22T14:09:00Z
**Detail Level**: deep
**Change Name**: owasp-security-hardening

---

## Current State

The infrastructure is a GCP-based insurance carrier appetite matching engine composed of two Cloud Run services (API + Worker), Cloud Spanner, Pub/Sub, GCS, Artifact Registry, and a custom VPC. Authentication uses Workload Identity Federation for CI/CD. There is no application code yet — all findings are in Terraform IaC.

Security posture is weak across three critical dimensions:

1. **Access control** — The API Cloud Run service is publicly reachable without any authentication in both dev and prod. No project-level audit logging is configured. Secret Manager access is granted at the project level rather than per-secret.
2. **Network** — The VPC has no firewall rules whatsoever. No Cloud Armor (WAF/DDoS protection) is in place.
3. **Data protection** — No CMEK for Spanner, GCS, or Artifact Registry. The Terraform state bucket lacks `public_access_prevention`.
4. **CI/CD** — The `terraform plan` output (which can contain secret values) is posted verbatim to PR comments. The `continue-on-error: true` flag on the plan step masks failures. The Terraform SA has no explicitly-defined IAM permissions in IaC.
5. **Supply chain** — All `versions.tf` files in modules use `~> 6.0` without a patch-pinned version. No container image scanning is configured on Artifact Registry.
6. **Observability** — Monitoring module accepts a single `service_name`; only the API service is ever wired to it. The Worker has no alerting.

---

## Relevant Files

| File Path | Purpose | Lines | Complexity | Test Coverage |
|-----------|---------|-------|------------|---------------|
| `terraform/modules/cloud-run/main.tf` | Cloud Run service + IAM (allUsers invoker gate) | 79 | Low | None |
| `terraform/modules/cloud-run/variables.tf` | Cloud Run variable declarations incl. `allow_unauthenticated` | 109 | Low | None |
| `terraform/modules/networking/main.tf` | VPC, subnet, VPC connector — no firewall resources | 51 | Low | None |
| `terraform/modules/iam/main.tf` | Service accounts + project-scoped IAM bindings | 50 | Low | None |
| `terraform/modules/storage/main.tf` | GCS bucket, Artifact Registry repo, bucket IAM — no AR IAM | 54 | Low | None |
| `terraform/modules/monitoring/main.tf` | Alert policies (latency, error rate, CPU, uptime) for single service | 173 | Medium | None |
| `terraform/modules/monitoring/variables.tf` | Monitoring inputs — single `service_name` and `service_url` | 26 | Low | None |
| `terraform/bootstrap/main.tf` | State bucket, WIF pool/provider, Terraform SA — no SA roles | 83 | Low | None |
| `terraform/bootstrap/versions.tf` | Bootstrap provider pinning (`~> 6.0`) | 10 | Low | None |
| `terraform/modules/cloud-run/versions.tf` | Module provider pinning (`~> 6.0`, no patch pin) | 8 | Low | None |
| `terraform/modules/iam/versions.tf` | Module provider pinning (`~> 6.0`, no patch pin) | 8 | Low | None |
| `terraform/modules/networking/versions.tf` | Module provider pinning (`~> 6.0`, no patch pin) | 8 | Low | None |
| `terraform/modules/spanner/versions.tf` | Module provider pinning (`~> 6.0`, no patch pin) | 8 | Low | None |
| `terraform/modules/pubsub/versions.tf` | Module provider pinning (`~> 6.0`, no patch pin) | 8 | Low | None |
| `terraform/modules/storage/versions.tf` | Module provider pinning (`~> 6.0`, no patch pin) | 8 | Low | None |
| `terraform/modules/monitoring/versions.tf` | Module provider pinning (`~> 6.0`, no patch pin) | 8 | Low | None |
| `terraform/environments/dev/main.tf` | Dev environment wiring — `allow_unauthenticated = true` | 108 | Low | None |
| `terraform/environments/prod/main.tf` | Prod environment wiring — `allow_unauthenticated = true` | 108 | Low | None |
| `.github/workflows/terraform.yml` | CI/CD: plan + apply pipeline with PR comment step | 136 | Medium | None |

---

## Dependency Map

```
environments/dev  ──────────────────────────────────────────────────────────┐
environments/prod ──────────────────────────────────────────────────────────┤
                                                                             ▼
                   modules/iam ──────────────────────────────► modules/cloud-run
                        │  (sa emails)                              │
                        └──────────────────────────────────────────┤
                                                                    ▼
                   modules/networking ──(vpc_connector_id)──► modules/cloud-run
                                                                    │
                   modules/spanner ◄────────────────────────────────┤ (env_vars)
                   modules/pubsub  ◄────────────────────────────────┘
                   modules/storage ◄──── (registry_url, bucket IAM)
                   modules/monitoring ◄─ (service_name, service_url of API only)

bootstrap/ ─────── independent root; manages state bucket + WIF + Terraform SA
                   (no IAM role assignments for Terraform SA)
```

Security controls that span multiple modules:
- `allow_unauthenticated` flows: env `main.tf` → `cloud-run/main.tf:61-68` (allUsers member)
- Secret Manager access: `iam/main.tf:34` (project scope) → every secret in the project
- Audit log gap: no `google_project_iam_audit_config` in any module or environment root
- Firewall gap: `networking/main.tf` creates VPC (line 15) and subnet (line 25) but zero `google_compute_firewall` resources

---

## Data Flow

```
Internet
   │
   ▼  (no Cloud Armor, no auth — C1, M1)
Cloud Run API  ──INGRESS_TRAFFIC_ALL, allUsers──►  [public]
   │
   │  (egress via VPC connector)
   ▼
VPC (no firewall rules — C2)
   │
   ├──► Cloud Spanner  (no CMEK — H1)
   ├──► Pub/Sub topic
   └──► GCS documents bucket  (no CMEK — H1)

Pub/Sub ──OIDC push──► Cloud Run Worker  (INGRESS_TRAFFIC_INTERNAL_ONLY, auth OK)

Secret Manager
   └── project-level secretAccessor for both SAs (H2)
       (all secrets exposed to both SAs; should be per-secret)

CI/CD Pipeline
   PR open → terraform plan (continue-on-error:true — M2)
           → plan stdout → PR comment verbatim (H4)
   push main → terraform apply (dev → prod)
   Terraform SA has no project roles defined in IaC (H3)

Artifact Registry
   └── no vulnerability scanning policy (L1)
   └── no IAM scoping (H5) — no `google_artifact_registry_repository_iam_member`
```

---

## Risk Assessment

| Dimension | Level | Notes |
|-----------|-------|-------|
| Authentication | CRITICAL | API is allUsers in both dev and prod |
| Network isolation | CRITICAL | Zero firewall rules on VPC |
| Audit / compliance | CRITICAL | No `google_project_iam_audit_config`; admin and data access unlogged |
| IAM least-privilege | HIGH | `secretmanager.secretAccessor` is project-scoped; both SAs see all secrets |
| Data encryption | HIGH | No CMEK on Spanner, GCS, or Artifact Registry |
| CI/CD secret exposure | HIGH | Plan stdout (can contain secret values) posted to public PR comments |
| IaC completeness | HIGH | Terraform SA roles not in code; Artifact Registry has no IAM; state bucket missing `public_access_prevention` |
| Supply chain | MEDIUM | `~> 6.0` allows any 6.x.x provider; patch-level breaking changes possible |
| Observability | MEDIUM | Worker has no monitoring; `continue-on-error` masks plan failures |
| DDoS / rate-limit | MEDIUM | No Cloud Armor; exposed API has no request throttling |
| Image security | LOW | No Artifact Registry vulnerability scanning policy |
| Connector sizing | LOW | `e2-micro` VPC connector; under-provisioned for prod traffic |

Overall blast radius if C1 + C2 + C3 are exploited simultaneously: unauthenticated remote code execution vector against API, no network controls to limit lateral movement, and no audit trail to detect or reconstruct an incident.

---

## Approach Comparison

Single approach: fix all findings in their respective modules, no new modules required.

**Why single approach is correct here:**
- Every finding maps cleanly to an existing file. There is no cross-cutting architectural change that would justify a new module (Cloud Armor could be a new `modules/armor` but can equally live inside `cloud-run` or `networking`).
- The findings are independent — each can be addressed without affecting the others.
- Adding new modules increases the blast radius of this PR; in-place fixes are safer for a security change.

The only judgment call is where Cloud Armor (M1) lives: it is a network-layer control so `modules/networking` is the natural home, with the backend service referencing the Cloud Run URL. Alternatively it could be a standalone `modules/armor` for reuse. Either is valid; `modules/networking` is simpler and avoids a new module for a single resource.

---

## Recommendation

Fix all findings, prioritized by severity. Recommended order within a single PR:

**Phase 1 — Critical (must ship together; each blocks the others from being meaningful alone):**

1. **C1** — `terraform/modules/cloud-run/main.tf` + `terraform/environments/dev/main.tf:52` + `terraform/environments/prod/main.tf:52`: Set `allow_unauthenticated = false` for the API in both environments. Add a `google_cloud_endpoints_service` or use Cloud Run IAP / Identity-Aware Proxy, or gate with a dedicated load balancer SA as invoker. At minimum remove `allUsers`; the exact auth mechanism is an open question (see below).
2. **C2** — `terraform/modules/networking/main.tf`: Add `google_compute_firewall` rules: deny-all ingress default, allow egress to GCP private APIs (199.36.153.8/30), deny all other egress.
3. **C3** — `terraform/modules/iam/main.tf` or a new `audit.tf`: Add `google_project_iam_audit_config` for `allServices` enabling `DATA_READ`, `DATA_WRITE`, and `ADMIN_READ` log types.

**Phase 2 — High:**

4. **H1** — `terraform/modules/storage/main.tf` and a new `terraform/modules/kms/main.tf` (or inline in storage/spanner modules): Add `google_kms_key_ring` + `google_kms_crypto_key`; wire CMEK into GCS bucket (`encryption { default_kms_key_name }`), Spanner instance (`encryption_config`), and Artifact Registry (`kms_key_name`).
5. **H2** — `terraform/modules/iam/main.tf:34`: Remove `roles/secretmanager.secretAccessor` from the project-scoped `sa_project_roles` list. Add `google_secret_manager_secret_iam_member` per-secret bindings in the modules that own each secret (or pass secret names as variables to `iam` module).
6. **H3** — `terraform/bootstrap/main.tf` after line 83: Add `google_project_iam_member` resources granting the Terraform SA exactly the roles it needs (`roles/editor` is too broad; enumerate: `roles/run.admin`, `roles/spanner.admin`, `roles/storage.admin`, etc.).
7. **H4** — `.github/workflows/terraform.yml:54-75`: Save plan output to a file; strip or redact before posting. Use `terraform show -no-color tfplan` and pipe through a redaction script, or post only the summary line count rather than full stdout. Remove `continue-on-error: true` (line 55) — see M2.
8. **H5** — `terraform/modules/storage/main.tf`: Add `google_artifact_registry_repository_iam_member` granting `roles/artifactregistry.writer` to the API/Worker SAs only, and `roles/artifactregistry.reader` to the Cloud Run SA.

**Phase 3 — Medium:**

9. **M1** — `terraform/modules/networking/main.tf`: Add `google_compute_security_policy` (Cloud Armor) with rate-limiting rule (e.g. 1000 req/min per IP) and attach to a Cloud Load Balancing frontend. Note: Cloud Armor requires a GLB — this may require adding a `google_compute_backend_service` wrapping the Cloud Run NEG, which is a larger change.
10. **M2** — `.github/workflows/terraform.yml:55`: Remove `continue-on-error: true`; the `Plan Status` step at line 77 already handles the failure gate — the flag is redundant and dangerous.
11. **M3** — `terraform/bootstrap/main.tf:10-35`: Add `public_access_prevention = "enforced"` to the `google_storage_bucket.terraform_state` resource.
12. **M4** — `terraform/modules/monitoring/main.tf` + `terraform/modules/monitoring/variables.tf`: Change `service_name`/`service_url` to accept a list of services, or instantiate the monitoring module twice in each environment (once for API, once for Worker). Wire Worker in `terraform/environments/dev/main.tf` and `terraform/environments/prod/main.tf`.
13. **M5** — All `versions.tf` files: Pin to a specific patch version, e.g. `version = "6.14.1"` (or whatever the current stable is), and use `~> 6.14.1` (patch-level pessimistic constraint) instead of `~> 6.0` (minor-level).

**Phase 4 — Low:**

14. **L1** — `terraform/modules/storage/main.tf`: Add `google_artifact_registry_repository` attribute `vulnerability_scanning = true` (or `remote_repository_config` with scanning enabled depending on provider version).
15. **L2** — `terraform/modules/networking/main.tf:48`: Change `machine_type = "e2-micro"` to `"e2-standard-4"` for prod (or make it a variable with env-specific override); `e2-micro` caps at 200 Mbps throughput and is insufficient for prod.

---

## Open Questions (DEFERRED)

1. **C1 auth mechanism**: What is the intended client auth model for the API? Options are (a) Cloud Run IAP, (b) API key via Cloud Endpoints / API Gateway, (c) mutual TLS, (d) JWT verified in-app. The correct fix for C1 depends on this answer — removing `allUsers` is unambiguous, but what to replace it with requires a product decision.

2. **M1 GLB requirement**: Cloud Armor requires a Global External Application Load Balancer. Adding a GLB in front of Cloud Run is a significant infrastructure change (adds `google_compute_global_address`, `google_compute_backend_service`, `google_compute_url_map`, `google_compute_target_https_proxy`, `google_compute_forwarding_rule`, `google_compute_region_network_endpoint_group`). Should this be a separate change from the core OWASP hardening, or bundled here?

3. **H1 KMS key hierarchy**: One key ring per environment? One key per service (Spanner, GCS, AR)? Cross-project KMS? Key rotation period? These decisions affect the KMS module design.

4. **H2 secret inventory**: No Secret Manager secrets are defined in this IaC yet (secrets are referenced by name in `secret_env_vars` but not created). Once secrets are added, per-secret IAM bindings need to be co-located with the `google_secret_manager_secret` resource. Confirm whether secrets will be managed in Terraform or externally.

5. **H3 Terraform SA minimum roles**: The Terraform SA needs to manage Cloud Run, Spanner, Pub/Sub, GCS, Artifact Registry, IAM, VPC, Monitoring, KMS, and Secret Manager. The exact minimal role set should be enumerated before writing the bootstrap IAM; `roles/editor` + a few admin roles is common but should be reviewed against a principle-of-least-privilege checklist.
