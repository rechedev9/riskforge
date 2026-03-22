# Delta Spec: Monitoring

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The monitoring module provisions alert policies (latency, error rate, CPU utilization), uptime checks, and notification channels. It follows the reference patterns from `docs/terraform-gcp.md` with alert policies using `google_monitoring_alert_policy` and email-only notification channels as the initial channel type. The module splits notification channels into a separate `channels.tf` file.

## ADDED Requirements

### REQ-MON-001: Email Notification Channel

The monitoring module **MUST** create a `google_monitoring_notification_channel` of type `"email"` with `labels.email_address = var.notification_email`. The channel resource **MUST** reside in `channels.tf` (not `main.tf`).

#### Scenario: Email notification channel in channels.tf · `code-based` · `critical`

- **WHEN** `terraform/modules/monitoring/channels.tf` is read
- **THEN** a `google_monitoring_notification_channel` resource exists with `type = "email"` and `labels.email_address` referencing `var.notification_email`

#### Scenario: Channel not duplicated in main.tf · `code-based` · `critical`

- **WHEN** `terraform/modules/monitoring/main.tf` is searched for `google_monitoring_notification_channel`
- **THEN** no `google_monitoring_notification_channel` resources are declared in `main.tf`

---

### REQ-MON-002: Latency Alert Policy

The monitoring module **MUST** create a `google_monitoring_alert_policy` for Cloud Run P99 latency exceeding a threshold. The alert **MUST** reference `metric.type = "run.googleapis.com/request_latencies"` and filter by the service name variable. The alert **MUST** reference the email notification channel.

#### Scenario: Latency alert policy created · `code-based` · `critical`

- **WHEN** `terraform/modules/monitoring/main.tf` is read
- **THEN** a `google_monitoring_alert_policy` resource exists with a `condition_threshold.filter` containing `"run.googleapis.com/request_latencies"`

#### Scenario: Latency alert references notification channel · `code-based` · `critical`

- **WHEN** the latency alert policy is inspected
- **THEN** `notification_channels` includes a reference to the email notification channel resource

---

### REQ-MON-003: Error Rate Alert Policy

The monitoring module **MUST** create a `google_monitoring_alert_policy` for Cloud Run 5xx error rate. The alert **MUST** reference `metric.type = "run.googleapis.com/request_count"` filtered to `response_code_class = "5xx"`.

#### Scenario: Error rate alert policy created · `code-based` · `critical`

- **WHEN** `terraform/modules/monitoring/main.tf` is read
- **THEN** a `google_monitoring_alert_policy` resource exists with a filter containing `"run.googleapis.com/request_count"` and `"5xx"`

---

### REQ-MON-004: CPU Utilization Alert Policy

The monitoring module **MUST** create a `google_monitoring_alert_policy` for Cloud Run CPU utilization exceeding 80%.

#### Scenario: CPU alert policy created · `code-based` · `critical`

- **WHEN** `terraform/modules/monitoring/main.tf` is read
- **THEN** a `google_monitoring_alert_policy` resource exists with a filter containing `"run.googleapis.com/container/cpu/utilizations"` and `threshold_value` of `0.8`

---

### REQ-MON-005: Uptime Check

The monitoring module **MUST** create a `google_monitoring_uptime_check_config` targeting the Cloud Run service's `/health` endpoint on port 443 with SSL validation. The service host **MUST** be derived from `var.service_url`.

#### Scenario: Uptime check targets /health endpoint · `code-based` · `critical`

- **WHEN** `terraform/modules/monitoring/main.tf` is read
- **THEN** a `google_monitoring_uptime_check_config` resource exists with `http_check.path = "/health"`, `http_check.port = 443`, and `http_check.use_ssl = true`

#### Scenario: Uptime check host derived from service URL · `code-based` · `critical`

- **WHEN** the uptime check's `monitored_resource.labels.host` is inspected
- **THEN** it references `var.service_url` (with `trimprefix` to remove `"https://"`)

---

### REQ-MON-006: Monitoring Module Structure

The monitoring module **MUST** contain `main.tf`, `channels.tf`, `variables.tf`, `outputs.tf`, and `versions.tf`. This is an exception to the standard 4-file module structure; the extra `channels.tf` follows the reference doc pattern.

#### Scenario: Monitoring module files present · `code-based` · `critical`

- **WHEN** the contents of `terraform/modules/monitoring/` are listed
- **THEN** `main.tf`, `channels.tf`, `variables.tf`, `outputs.tf`, and `versions.tf` exist

---

### REQ-MON-007: Monitoring Module Outputs

The monitoring module **MUST** output `notification_channel_id` and `alert_policy_ids`.

#### Scenario: Monitoring outputs declared · `code-based` · `critical`

- **WHEN** `terraform/modules/monitoring/outputs.tf` is read
- **THEN** it declares `output "notification_channel_id"` and `output "alert_policy_ids"`

---

### REQ-MON-008: Monitoring Variables

The monitoring module **MUST** accept `var.project_id`, `var.service_name`, `var.service_url`, and `var.notification_email` as input variables. No hardcoded email addresses or service names **MUST** exist in any `.tf` file.

#### Scenario: Monitoring variables declared · `code-based` · `critical`

- **WHEN** `terraform/modules/monitoring/variables.tf` is read
- **THEN** it declares `variable "project_id"`, `variable "service_name"`, `variable "service_url"`, and `variable "notification_email"`

---

## MODIFIED Requirements

(none)

## REMOVED Requirements

(none)

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-MON-001    | ADDED | MUST     | 2         |
| REQ-MON-002    | ADDED | MUST     | 2         |
| REQ-MON-003    | ADDED | MUST     | 1         |
| REQ-MON-004    | ADDED | MUST     | 1         |
| REQ-MON-005    | ADDED | MUST     | 2         |
| REQ-MON-006    | ADDED | MUST     | 1         |
| REQ-MON-007    | ADDED | MUST     | 1         |
| REQ-MON-008    | ADDED | MUST     | 1         |

**Total Requirements**: 8
**Total Scenarios**: 11

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-MON-001 > Email notification channel in channels.tf | code-based | critical | pass^3 = 1.00 |
| REQ-MON-001 > Channel not duplicated in main.tf | code-based | critical | pass^3 = 1.00 |
| REQ-MON-002 > Latency alert policy created | code-based | critical | pass^3 = 1.00 |
| REQ-MON-002 > Latency alert references notification channel | code-based | critical | pass^3 = 1.00 |
| REQ-MON-003 > Error rate alert policy created | code-based | critical | pass^3 = 1.00 |
| REQ-MON-004 > CPU alert policy created | code-based | critical | pass^3 = 1.00 |
| REQ-MON-005 > Uptime check targets /health endpoint | code-based | critical | pass^3 = 1.00 |
| REQ-MON-005 > Uptime check host derived from service URL | code-based | critical | pass^3 = 1.00 |
| REQ-MON-006 > Monitoring module files present | code-based | critical | pass^3 = 1.00 |
| REQ-MON-007 > Monitoring outputs declared | code-based | critical | pass^3 = 1.00 |
| REQ-MON-008 > Monitoring variables declared | code-based | critical | pass^3 = 1.00 |
