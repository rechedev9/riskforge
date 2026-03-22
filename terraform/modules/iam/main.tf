# -----------------------------------------------------------------------------
# Service Accounts
# -----------------------------------------------------------------------------

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

# -----------------------------------------------------------------------------
# API SA IAM Bindings (5 roles)
# -----------------------------------------------------------------------------

resource "google_project_iam_member" "api_spanner" {
  project = var.project_id
  role    = "roles/spanner.databaseUser"
  member  = "serviceAccount:${google_service_account.cloud_run_api.email}"
}

resource "google_project_iam_member" "api_secretmanager" {
  project = var.project_id
  role    = "roles/secretmanager.secretAccessor"
  member  = "serviceAccount:${google_service_account.cloud_run_api.email}"
}

resource "google_project_iam_member" "api_storage" {
  project = var.project_id
  role    = "roles/storage.objectUser"
  member  = "serviceAccount:${google_service_account.cloud_run_api.email}"
}

resource "google_project_iam_member" "api_logging" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.cloud_run_api.email}"
}

resource "google_project_iam_member" "api_tracing" {
  project = var.project_id
  role    = "roles/cloudtrace.agent"
  member  = "serviceAccount:${google_service_account.cloud_run_api.email}"
}

# -----------------------------------------------------------------------------
# Worker SA IAM Bindings (5 roles)
# -----------------------------------------------------------------------------

resource "google_project_iam_member" "worker_spanner" {
  project = var.project_id
  role    = "roles/spanner.databaseUser"
  member  = "serviceAccount:${google_service_account.cloud_run_worker.email}"
}

resource "google_project_iam_member" "worker_secretmanager" {
  project = var.project_id
  role    = "roles/secretmanager.secretAccessor"
  member  = "serviceAccount:${google_service_account.cloud_run_worker.email}"
}

resource "google_project_iam_member" "worker_logging" {
  project = var.project_id
  role    = "roles/logging.logWriter"
  member  = "serviceAccount:${google_service_account.cloud_run_worker.email}"
}

resource "google_project_iam_member" "worker_tracing" {
  project = var.project_id
  role    = "roles/cloudtrace.agent"
  member  = "serviceAccount:${google_service_account.cloud_run_worker.email}"
}

resource "google_project_iam_member" "worker_pubsub" {
  project = var.project_id
  role    = "roles/pubsub.subscriber"
  member  = "serviceAccount:${google_service_account.cloud_run_worker.email}"
}

# -----------------------------------------------------------------------------
# Invoker SA IAM Binding (1 role)
# -----------------------------------------------------------------------------

resource "google_project_iam_member" "invoker_run" {
  project = var.project_id
  role    = "roles/run.invoker"
  member  = "serviceAccount:${google_service_account.pubsub_invoker.email}"
}
