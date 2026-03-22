# -----------------------------------------------------------------------------
# Spanner Instance
# -----------------------------------------------------------------------------

resource "google_spanner_instance" "main" {
  name             = "appetite-engine-${var.environment}"
  config           = "regional-${var.region}"
  display_name     = "Appetite Engine (${var.environment})"
  processing_units = var.spanner_processing_units
  project          = var.project_id
  force_destroy    = false

  labels = {
    environment = var.environment
  }
}

# -----------------------------------------------------------------------------
# Spanner Database with DDL
# -----------------------------------------------------------------------------

resource "google_spanner_database" "main" {
  instance                 = google_spanner_instance.main.name
  name                     = "appetite-engine-${var.environment}"
  project                  = var.project_id
  version_retention_period = "3d"
  database_dialect         = "GOOGLE_STANDARD_SQL"
  deletion_protection      = var.deletion_protection

  ddl = [
    <<-EOT
      CREATE TABLE Carriers (
        CarrierId STRING(36) NOT NULL DEFAULT (GENERATE_UUID()),
        Name STRING(255) NOT NULL,
        Code STRING(50) NOT NULL,
        IsActive BOOL NOT NULL DEFAULT (true),
        CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
        UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
      ) PRIMARY KEY (CarrierId)
    EOT
    ,
    <<-EOT
      CREATE TABLE AppetiteRules (
        RuleId STRING(36) NOT NULL DEFAULT (GENERATE_UUID()),
        CarrierId STRING(36) NOT NULL,
        State STRING(2) NOT NULL,
        LineOfBusiness STRING(100) NOT NULL,
        ClassCode STRING(50),
        MinPremium FLOAT64,
        MaxPremium FLOAT64,
        IsActive BOOL NOT NULL DEFAULT (true),
        EligibilityCriteria JSON,
        CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp = true),
      ) PRIMARY KEY (CarrierId, RuleId),
        INTERLEAVE IN PARENT Carriers ON DELETE CASCADE
    EOT
    ,
  ]

  lifecycle {
    ignore_changes = [ddl]
  }
}

# -----------------------------------------------------------------------------
# Database IAM Binding
# -----------------------------------------------------------------------------

resource "google_spanner_database_iam_binding" "database_user" {
  instance = google_spanner_instance.main.name
  database = google_spanner_database.main.name
  role     = "roles/spanner.databaseUser"
  project  = var.project_id

  members = [for email in var.sa_emails : "serviceAccount:${email}"]
}
