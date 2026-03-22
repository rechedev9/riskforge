resource "google_storage_bucket" "documents" {
  name     = "${var.project_id}-documents-${var.environment}"
  location = var.region
  project  = var.project_id

  storage_class               = "STANDARD"
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"

  force_destroy = false

  versioning {
    enabled = true
  }

  # Delete non-current versions after 90 days
  lifecycle_rule {
    condition {
      age        = 90
      with_state = "ARCHIVED"
    }
    action {
      type = "Delete"
    }
  }

  labels = {
    environment = var.environment
  }
}

resource "google_artifact_registry_repository" "docker" {
  repository_id = "appetite-engine-${var.environment}"
  format        = "DOCKER"
  location      = var.region
  project       = var.project_id
  description   = "Docker repository for appetite-engine images"

  vulnerability_scanning_config {
    enablement_config = "INHERITED"
  }

  labels = {
    environment = var.environment
  }
}

resource "google_storage_bucket_iam_member" "api_objectuser" {
  bucket = google_storage_bucket.documents.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${var.api_sa_email}"
}

resource "google_storage_bucket_iam_member" "worker_objectuser" {
  bucket = google_storage_bucket.documents.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${var.worker_sa_email}"
}

# H5: Scope Artifact Registry access to the runtime SAs only.
# Both API and Worker SAs need to pull their own images at Cloud Run startup.
resource "google_artifact_registry_repository_iam_member" "api_reader" {
  repository = google_artifact_registry_repository.docker.name
  location   = var.region
  project    = var.project_id
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${var.api_sa_email}"
}

resource "google_artifact_registry_repository_iam_member" "worker_reader" {
  repository = google_artifact_registry_repository.docker.name
  location   = var.region
  project    = var.project_id
  role       = "roles/artifactregistry.reader"
  member     = "serviceAccount:${var.worker_sa_email}"
}
