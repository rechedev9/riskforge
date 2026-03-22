# -----------------------------------------------------------------------------
# Enable VPC Access API
# -----------------------------------------------------------------------------

resource "google_project_service" "vpcaccess" {
  project            = var.project_id
  service            = "vpcaccess.googleapis.com"
  disable_on_destroy = false
}

# -----------------------------------------------------------------------------
# VPC Network
# -----------------------------------------------------------------------------

resource "google_compute_network" "vpc" {
  name                    = "appetite-engine-vpc-${var.environment}"
  project                 = var.project_id
  auto_create_subnetworks = false
}

# -----------------------------------------------------------------------------
# Subnet (/28 for VPC connector)
# -----------------------------------------------------------------------------

resource "google_compute_subnetwork" "connector_subnet" {
  name          = "vpc-connector-subnet-${var.environment}"
  ip_cidr_range = "10.8.0.0/28"
  region        = var.region
  network       = google_compute_network.vpc.id
  project       = var.project_id
}

# -----------------------------------------------------------------------------
# VPC Access Connector
# -----------------------------------------------------------------------------

resource "google_vpc_access_connector" "connector" {
  name    = "vpc-connector-${var.environment}"
  region  = var.region
  project = var.project_id

  subnet {
    name = google_compute_subnetwork.connector_subnet.name
  }

  min_instances = 2
  max_instances = 10
  machine_type  = "e2-micro"

  depends_on = [google_project_service.vpcaccess]
}
