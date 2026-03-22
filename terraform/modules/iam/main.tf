resource "google_service_account" "cloud_run_api" {
  account_id   = "cloud-run-api-${var.environment}"
  display_name = "Cloud Run API (${var.environment})"
  description  = "Runtime SA for the API Cloud Run service"
  project      = var.project_id
}

resource "google_service_account" "cloud_run_worker" {
  account_id   = "cloud-run-worker-${var.environment}"
  display_name = "Cloud Run Worker (${var.environment})"
  description  = "Runtime SA for the Worker Cloud Run service"
  project      = var.project_id
}

resource "google_service_account" "pubsub_invoker" {
  account_id   = "pubsub-invoker-${var.environment}"
  display_name = "Pub/Sub Push Invoker (${var.environment})"
  description  = "SA used for OIDC-authenticated push to Cloud Run"
  project      = var.project_id
}

locals {
  # Only project-level roles that cannot be scoped to individual resources.
  # Resource-scoped IAM (spanner, storage, pubsub, run.invoker) is handled
  # by each respective module for least-privilege.
  sa_project_roles = flatten([
    for sa_key, sa_email in {
      api    = google_service_account.cloud_run_api.email
      worker = google_service_account.cloud_run_worker.email
      } : [
      for role in [
        "roles/logging.logWriter",
        "roles/cloudtrace.agent",
        "roles/secretmanager.secretAccessor",
        ] : {
        key   = "${sa_key}-${replace(role, "roles/", "")}"
        email = sa_email
        role  = role
      }
    ]
  ])
}

resource "google_project_iam_member" "sa_roles" {
  for_each = { for binding in local.sa_project_roles : binding.key => binding }

  project = var.project_id
  role    = each.value.role
  member  = "serviceAccount:${each.value.email}"
}
