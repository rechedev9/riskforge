# Delta Spec: Monitoring Worker

**Change**: owasp-security-hardening
**Date**: 2026-03-22T15:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

`modules/monitoring/` accepts a single `service_name` and `service_url`, and both `environments/dev/main.tf` (line 101–107) and `environments/prod/main.tf` (line 100–107) wire only `module.cloud_run_api` to it. The Worker Cloud Run service has no latency, error-rate, CPU, or uptime alerting. This spec covers M4: extending the monitoring module to optionally accept Worker service inputs and wiring the Worker in both environments.

## ADDED Requirements

### REQ-MON-001: Monitoring Module Accepts Worker Service Inputs

`modules/monitoring/variables.tf` **MUST** declare two new optional variables: `worker_service_name` of type `string` with `default = ""`, and `worker_service_url` of type `string` with `default = ""`.

Both variables **MUST** have a `description` attribute.

Adding these variables **MUST** be backward compatible: existing callers that do not pass these variables **MUST** continue to work without modification (the `default = ""` ensures this).

#### Scenario: New variables declared with empty defaults · `code-based` · `critical`

- **WHEN** `modules/monitoring/variables.tf` is inspected
- **THEN** it contains `variable "worker_service_name"` with `default = ""` and `variable "worker_service_url"` with `default = ""`

#### Scenario: Existing callers pass terraform validate without error · `code-based` · `critical`

- **GIVEN** a caller that passes only `service_name`, `service_url`, `project_id`, `notification_email`, and `enable_alerts` (no worker variables)
- **WHEN** `terraform validate` is run in `terraform/environments/dev/`
- **THEN** the command exits with code `0` and no error about missing required variables

### REQ-MON-002: Worker Alert Policies Created When worker_service_name Is Set

`modules/monitoring/main.tf` **MUST** contain alert policy resources for the Worker service (at minimum: latency, error rate, and CPU utilization) gated on `var.worker_service_name != ""`.

The Worker alert policies **MUST** use `count = (var.enable_alerts && var.worker_service_name != "") ? 1 : 0` or an equivalent `for_each` conditional.

The Worker alert policies **MUST** filter on `resource.labels.service_name = "${var.worker_service_name}"` — they **MUST NOT** reuse the API's `var.service_name` filter.

#### Scenario: Worker latency alert appears in plan when worker_service_name is set · `code-based` · `critical`

- **GIVEN** `terraform/environments/prod/main.tf` passes `worker_service_name = module.cloud_run_worker.service_name` to the monitoring module
- **WHEN** `terraform plan -no-color` is run in `terraform/environments/prod/`
- **THEN** the plan output contains a `google_monitoring_alert_policy` resource with display name containing `worker` and a filter containing `service_name = "appetite-engine-worker-prod"`

#### Scenario: Worker alerts absent when worker_service_name is empty string · `code-based` · `standard`

- **GIVEN** `worker_service_name = ""` (default) is in effect for an environment
- **WHEN** `terraform plan -no-color` is run
- **THEN** no `google_monitoring_alert_policy` resource for the Worker appears in the plan

### REQ-MON-003: Worker Uptime Check Created When worker_service_url Is Set

`modules/monitoring/main.tf` **MUST** contain a `google_monitoring_uptime_check_config` resource for the Worker, gated on `var.enable_alerts && var.worker_service_url != ""`.

The uptime check **MUST** target `trimprefix(var.worker_service_url, "https://")` as the `host` label.

#### Scenario: Worker uptime check in plan when worker_service_url is set · `code-based` · `standard`

- **GIVEN** both `worker_service_name` and `worker_service_url` are non-empty in the monitoring module call
- **WHEN** `terraform plan -no-color` is run in `terraform/environments/prod/`
- **THEN** the plan output contains `google_monitoring_uptime_check_config` with display name containing `worker`

### REQ-MON-004: Dev and Prod Environments Wire Worker to Monitoring

`terraform/environments/dev/main.tf` **MUST** pass `worker_service_name = module.cloud_run_worker.service_name` and `worker_service_url = module.cloud_run_worker.service_url` to `module.monitoring`.

`terraform/environments/prod/main.tf` **MUST** pass the same two arguments.

#### Scenario: Dev monitoring module call includes worker inputs · `code-based` · `critical`

- **WHEN** `terraform/environments/dev/main.tf` is inspected
- **THEN** the `module "monitoring"` block contains `worker_service_name = module.cloud_run_worker.service_name` and `worker_service_url = module.cloud_run_worker.service_url`

#### Scenario: Prod monitoring module call includes worker inputs · `code-based` · `critical`

- **WHEN** `terraform/environments/prod/main.tf` is inspected
- **THEN** the `module "monitoring"` block contains `worker_service_name = module.cloud_run_worker.service_name` and `worker_service_url = module.cloud_run_worker.service_url`

### REQ-MON-005: Existing API Alert Policies Unchanged

The addition of Worker monitoring variables and resources **MUST NOT** modify the existing API alert policy resources in `modules/monitoring/main.tf`.

The `google_monitoring_alert_policy.latency`, `google_monitoring_alert_policy.error_rate`, `google_monitoring_alert_policy.cpu_utilization`, and `google_monitoring_uptime_check_config.api_health` resources **MUST** remain functionally identical to their current state (same filters, thresholds, durations).

#### Scenario: Plan shows no changes to existing API alert resources · `code-based` · `critical`

- **GIVEN** the current state has the API monitoring resources created in dev with `enable_alerts = false`
- **WHEN** `terraform plan -no-color` is run in `terraform/environments/dev/` after only the worker inputs are added
- **THEN** no `~ module.monitoring.google_monitoring_alert_policy.latency[0]` update appears in the plan

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-MON-001    | ADDED | MUST     | 2         |
| REQ-MON-002    | ADDED | MUST     | 2         |
| REQ-MON-003    | ADDED | SHOULD   | 1         |
| REQ-MON-004    | ADDED | MUST     | 2         |
| REQ-MON-005    | ADDED | MUST     | 1         |

**Total Requirements**: 5
**Total Scenarios**: 8

---

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-MON-001 › New variables declared with empty defaults | code-based | critical | pass^3 = 1.00 |
| REQ-MON-001 › Existing callers pass terraform validate without error | code-based | critical | pass^3 = 1.00 |
| REQ-MON-002 › Worker latency alert appears in plan when worker_service_name is set | code-based | critical | pass^3 = 1.00 |
| REQ-MON-002 › Worker alerts absent when worker_service_name is empty string | code-based | standard | pass@3 ≥ 0.90 |
| REQ-MON-003 › Worker uptime check in plan when worker_service_url is set | code-based | standard | pass@3 ≥ 0.90 |
| REQ-MON-004 › Dev monitoring module call includes worker inputs | code-based | critical | pass^3 = 1.00 |
| REQ-MON-004 › Prod monitoring module call includes worker inputs | code-based | critical | pass^3 = 1.00 |
| REQ-MON-005 › Plan shows no changes to existing API alert resources | code-based | critical | pass^3 = 1.00 |
