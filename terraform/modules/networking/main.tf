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
  machine_type  = var.connector_machine_type

  depends_on = [google_project_service.vpcaccess]
}

# -----------------------------------------------------------------------------
# Firewall Rules (C2)
# -----------------------------------------------------------------------------

# Deny all ingress — lowest-priority default; all specific allows above override this.
resource "google_compute_firewall" "deny_all_ingress" {
  name      = "deny-all-ingress-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "INGRESS"
  priority  = 65534

  deny {
    protocol = "all"
  }

  source_ranges = ["0.0.0.0/0"]
}

# Deny all egress — lowest-priority default.
resource "google_compute_firewall" "deny_all_egress" {
  name      = "deny-all-egress-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "EGRESS"
  priority  = 65534

  deny {
    protocol = "all"
  }

  destination_ranges = ["0.0.0.0/0"]
}

# Allow intra-subnet traffic (VPC connector instances communicating).
resource "google_compute_firewall" "allow_internal" {
  name      = "allow-internal-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "INGRESS"
  priority  = 1000

  allow {
    protocol = "tcp"
  }

  allow {
    protocol = "udp"
  }

  allow {
    protocol = "icmp"
  }

  source_ranges = [google_compute_subnetwork.connector_subnet.ip_cidr_range]
}

# Allow ingress from GCP health check ranges so the VPC connector passes health checks.
resource "google_compute_firewall" "allow_health_checks" {
  name      = "allow-health-checks-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "INGRESS"
  priority  = 1000

  allow {
    protocol = "tcp"
  }

  # GCP health check source ranges (documented at cloud.google.com/load-balancing/docs/health-checks)
  source_ranges = ["35.191.0.0/16", "130.211.0.0/22"]
}

# Allow egress to restricted.googleapis.com so Cloud Run can reach Spanner, Pub/Sub, GCS, etc.
resource "google_compute_firewall" "allow_gcp_apis" {
  name      = "allow-gcp-apis-egress-${var.environment}"
  network   = google_compute_network.vpc.id
  project   = var.project_id
  direction = "EGRESS"
  priority  = 1000

  allow {
    protocol = "tcp"
    ports    = ["443"]
  }

  # restricted.googleapis.com CIDR (199.36.153.8/30)
  destination_ranges = ["199.36.153.8/30"]
}

# -----------------------------------------------------------------------------
# Cloud Armor Security Policy (M1 — placeholder; not yet attached to a load balancer)
# Attachment requires a Global External Application Load Balancer.
# Track as follow-up change: glb-cloud-armor
# -----------------------------------------------------------------------------

resource "google_compute_security_policy" "rate_limit" {
  name    = "appetite-engine-rate-limit-${var.environment}"
  project = var.project_id

  rule {
    action   = "rate_based_ban"
    priority = 1000

    match {
      versioned_expr = "SRC_IPS_V1"

      config {
        src_ip_ranges = ["*"]
      }
    }

    rate_limit_options {
      rate_limit_threshold {
        count        = 1000
        interval_sec = 60
      }

      ban_duration_sec = 300

      conform_action = "allow"
      exceed_action  = "deny(429)"

      enforce_on_key = "IP"
    }

    description = "Rate limit: 1000 req/min per IP; ban 5 min on exceed"
  }

  rule {
    action   = "allow"
    priority = 2147483647

    match {
      versioned_expr = "SRC_IPS_V1"

      config {
        src_ip_ranges = ["*"]
      }
    }

    description = "Default allow rule"
  }
}
