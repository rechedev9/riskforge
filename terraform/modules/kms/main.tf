resource "google_kms_key_ring" "main" {
  count = var.enable_cmek ? 1 : 0

  name     = "appetite-engine-${var.environment}"
  location = var.region
  project  = var.project_id
}

resource "google_kms_crypto_key" "main" {
  count = var.enable_cmek ? 1 : 0

  name            = "appetite-engine-key-${var.environment}"
  key_ring        = google_kms_key_ring.main[0].id
  rotation_period = "7776000s" # 90 days

  lifecycle {
    prevent_destroy = true
  }
}
