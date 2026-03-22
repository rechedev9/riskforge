provider "google" {
  project = var.project_id
  region  = var.region
}

# -----------------------------------------------------------------------------
# GCS State Bucket
# -----------------------------------------------------------------------------

resource "google_storage_bucket" "terraform_state" {
  name     = "${var.project_id}-terraform-state"
  location = var.region
  project  = var.project_id

  force_destroy = false

  versioning {
    enabled = true
  }

  lifecycle_rule {
    condition {
      num_newer_versions = 5
    }
    action {
      type = "Delete"
    }
  }

  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  labels = {
    purpose = "terraform-state"
  }
}

# -----------------------------------------------------------------------------
# Workload Identity Federation
# -----------------------------------------------------------------------------

resource "google_iam_workload_identity_pool" "github" {
  workload_identity_pool_id = "github-actions"
  display_name              = "GitHub Actions"
  description               = "WIF pool for GitHub Actions OIDC"
  project                   = var.project_id
}

resource "google_iam_workload_identity_pool_provider" "github" {
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github-provider"
  display_name                       = "GitHub Actions OIDC"
  description                        = "GitHub Actions identity provider"

  attribute_condition = "attribute.repository == \"${var.github_org}/${var.github_repo}\""

  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.actor"      = "assertion.actor"
    "attribute.repository" = "assertion.repository"
    "attribute.ref"        = "assertion.ref"
  }

  oidc {
    issuer_uri = "https://token.actions.githubusercontent.com"
  }
}

# -----------------------------------------------------------------------------
# Terraform Service Account
# -----------------------------------------------------------------------------

resource "google_service_account" "terraform" {
  account_id   = "terraform"
  display_name = "Terraform Automation"
  description  = "SA for Terraform plan/apply via CI/CD"
  project      = var.project_id
}

resource "google_service_account_iam_member" "wif_terraform" {
  service_account_id = google_service_account.terraform.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.github_org}/${var.github_repo}"
}

# -----------------------------------------------------------------------------
# Terraform SA Project Roles (H3)
# Enumerated minimal set. No roles/editor — each role scoped to one service domain.
# -----------------------------------------------------------------------------

locals {
  terraform_sa_roles = toset([
    "roles/compute.admin",
    "roles/iam.serviceAccountAdmin",
    "roles/run.admin",
    "roles/spanner.admin",
    "roles/pubsub.admin",
    "roles/storage.admin",
    "roles/monitoring.admin",
    "roles/vpcaccess.admin",
    "roles/artifactregistry.admin",
    "roles/secretmanager.admin",
    "roles/iam.workloadIdentityPoolAdmin",
  ])
}

resource "google_project_iam_member" "terraform_roles" {
  for_each = local.terraform_sa_roles

  project = var.project_id
  role    = each.value
  member  = "serviceAccount:${google_service_account.terraform.email}"
}
