# Delta Spec: Pub/Sub

**Change**: terraform-gcp-module
**Date**: 2026-03-22T00:00:00Z
**Status**: draft
**Depends On**: proposal.md

---

## Context

The Pub/Sub module provisions topics, subscriptions, and IAM bindings for the appetite-engine event pipeline. The main topic (`appetite-events`) has a push subscription targeting the worker Cloud Run service with OIDC authentication. A dead-letter topic (DLQ) captures failed messages with a pull subscription for manual inspection.

## ADDED Requirements

### REQ-PSUB-001: Main Topic

The Pub/Sub module **MUST** create a `google_pubsub_topic` for appetite events. The topic name **SHOULD** be parameterized or derived from input variables. The topic **SHOULD** set `message_retention_duration`.

#### Scenario: Appetite events topic created · `code-based` · `critical`

- **WHEN** `terraform/modules/pubsub/main.tf` is read
- **THEN** a `google_pubsub_topic` resource exists for appetite events

---

### REQ-PSUB-002: Dead Letter Topic

The Pub/Sub module **MUST** create a separate `google_pubsub_topic` for the dead-letter queue (DLQ).

#### Scenario: DLQ topic created · `code-based` · `critical`

- **WHEN** `terraform/modules/pubsub/main.tf` is read
- **THEN** a second `google_pubsub_topic` resource exists with a name containing `"dlq"` or `"dead-letter"`

---

### REQ-PSUB-003: Push Subscription with OIDC

The Pub/Sub module **MUST** create a `google_pubsub_subscription` with `push_config` targeting the worker Cloud Run service. The `push_config` **MUST** include an `oidc_token` block with `service_account_email = var.invoker_sa_email`. The subscription **MUST** include a `dead_letter_policy` referencing the DLQ topic with `max_delivery_attempts`. The subscription **MUST** include a `retry_policy` with exponential backoff.

#### Scenario: Push subscription targets worker endpoint · `code-based` · `critical`

- **WHEN** `terraform/modules/pubsub/main.tf` is read
- **THEN** a `google_pubsub_subscription` resource has `push_config.push_endpoint` referencing `var.push_endpoint`

#### Scenario: Push subscription uses OIDC auth · `code-based` · `critical`

- **WHEN** the push subscription's `push_config` block is inspected
- **THEN** an `oidc_token` block exists with `service_account_email` referencing the invoker SA variable

#### Scenario: Push subscription has dead-letter policy · `code-based` · `critical`

- **WHEN** the push subscription is inspected
- **THEN** a `dead_letter_policy` block references the DLQ topic and sets `max_delivery_attempts`

#### Scenario: Push subscription has retry policy · `code-based` · `standard`

- **WHEN** the push subscription is inspected
- **THEN** a `retry_policy` block exists with `minimum_backoff` and `maximum_backoff` set

---

### REQ-PSUB-004: DLQ Pull Subscription

The Pub/Sub module **MUST** create a `google_pubsub_subscription` (pull mode, no `push_config`) on the DLQ topic for manual inspection of failed messages.

#### Scenario: DLQ pull subscription created · `code-based` · `critical`

- **WHEN** `terraform/modules/pubsub/main.tf` is read
- **THEN** a `google_pubsub_subscription` resource exists on the DLQ topic without a `push_config` block

---

### REQ-PSUB-005: Pub/Sub IAM

The Pub/Sub module **MUST** grant the worker SA `roles/pubsub.subscriber` on the push subscription. The Pub/Sub module **SHOULD** grant the API SA `roles/pubsub.publisher` on the main topic. The Pub/Sub module **MUST** grant the Pub/Sub service agent `roles/pubsub.publisher` on the DLQ topic and `roles/pubsub.subscriber` on the push subscription for dead-letter forwarding.

#### Scenario: Worker SA gets subscriber role · `code-based` · `critical`

- **WHEN** `terraform/modules/pubsub/main.tf` is read
- **THEN** a `google_pubsub_subscription_iam_member` grants `roles/pubsub.subscriber` to the worker SA

#### Scenario: DLQ publisher role for Pub/Sub service agent · `code-based` · `critical`

- **WHEN** `terraform/modules/pubsub/main.tf` is read
- **THEN** a `google_pubsub_topic_iam_member` grants `roles/pubsub.publisher` on the DLQ topic to the Pub/Sub service agent (`service-{project_number}@gcp-sa-pubsub.iam.gserviceaccount.com`)

---

### REQ-PSUB-006: Pub/Sub Module Outputs

The Pub/Sub module **MUST** output `topic_id`, `topic_name`, and `subscription_id`.

#### Scenario: Pub/Sub outputs declared · `code-based` · `critical`

- **WHEN** `terraform/modules/pubsub/outputs.tf` is read
- **THEN** it declares `output "topic_id"`, `output "topic_name"`, and `output "subscription_id"`

---

### REQ-PSUB-007: Pub/Sub Module Structure

The Pub/Sub module **MUST** contain `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf`.

#### Scenario: Pub/Sub module files present · `code-based` · `critical`

- **WHEN** the contents of `terraform/modules/pubsub/` are listed
- **THEN** `main.tf`, `variables.tf`, `outputs.tf`, and `versions.tf` exist

---

## MODIFIED Requirements

(none)

## REMOVED Requirements

(none)

---

## Acceptance Criteria Summary

| Requirement ID | Type  | Priority | Scenarios |
|----------------|-------|----------|-----------|
| REQ-PSUB-001   | ADDED | MUST     | 1         |
| REQ-PSUB-002   | ADDED | MUST     | 1         |
| REQ-PSUB-003   | ADDED | MUST     | 4         |
| REQ-PSUB-004   | ADDED | MUST     | 1         |
| REQ-PSUB-005   | ADDED | MUST     | 2         |
| REQ-PSUB-006   | ADDED | MUST     | 1         |
| REQ-PSUB-007   | ADDED | MUST     | 1         |

**Total Requirements**: 7
**Total Scenarios**: 11

## Eval Definitions

| Scenario | Eval Type | Criticality | Threshold |
|----------|-----------|-------------|-----------|
| REQ-PSUB-001 > Appetite events topic created | code-based | critical | pass^3 = 1.00 |
| REQ-PSUB-002 > DLQ topic created | code-based | critical | pass^3 = 1.00 |
| REQ-PSUB-003 > Push subscription targets worker endpoint | code-based | critical | pass^3 = 1.00 |
| REQ-PSUB-003 > Push subscription uses OIDC auth | code-based | critical | pass^3 = 1.00 |
| REQ-PSUB-003 > Push subscription has dead-letter policy | code-based | critical | pass^3 = 1.00 |
| REQ-PSUB-003 > Push subscription has retry policy | code-based | standard | pass@3 >= 0.90 |
| REQ-PSUB-004 > DLQ pull subscription created | code-based | critical | pass^3 = 1.00 |
| REQ-PSUB-005 > Worker SA gets subscriber role | code-based | critical | pass^3 = 1.00 |
| REQ-PSUB-005 > DLQ publisher role for Pub/Sub service agent | code-based | critical | pass^3 = 1.00 |
| REQ-PSUB-006 > Pub/Sub outputs declared | code-based | critical | pass^3 = 1.00 |
| REQ-PSUB-007 > Pub/Sub module files present | code-based | critical | pass^3 = 1.00 |
