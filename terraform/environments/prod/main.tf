locals {
  environment = "prod"
}

provider "google" {
  project = var.project_id
  region  = var.region
}

module "iam" {
  source      = "../../modules/iam"
  project_id  = var.project_id
  environment = local.environment
}

module "networking" {
  source                 = "../../modules/networking"
  project_id             = var.project_id
  region                 = var.region
  environment            = local.environment
  connector_machine_type = "e2-standard-4"
}

module "spanner" {
  source                   = "../../modules/spanner"
  project_id               = var.project_id
  region                   = var.region
  environment              = local.environment
  spanner_processing_units = 300
  deletion_protection      = true
  sa_emails                = [module.iam.api_sa_email, module.iam.worker_sa_email]
}

module "storage" {
  source          = "../../modules/storage"
  project_id      = var.project_id
  region          = var.region
  environment     = local.environment
  api_sa_email    = module.iam.api_sa_email
  worker_sa_email = module.iam.worker_sa_email
}

module "cloud_run_api" {
  source                = "../../modules/cloud-run"
  service_name          = "appetite-engine-api-${local.environment}"
  project_id            = var.project_id
  region                = var.region
  image                 = "${module.storage.registry_url}/api:${var.image_tag}"
  min_instances         = 2
  max_instances         = 20
  service_account_email = module.iam.api_sa_email
  ingress               = "INGRESS_TRAFFIC_ALL"
  allow_unauthenticated = true
  vpc_connector_id      = module.networking.vpc_connector_id
  deletion_protection   = true
  resource_limits       = { cpu = "2", memory = "1Gi" }
  env_vars = {
    SPANNER_INSTANCE = module.spanner.instance_name
    SPANNER_DATABASE = module.spanner.database_name
    PUBSUB_TOPIC     = module.pubsub.topic_name
    GCP_PROJECT      = var.project_id
    ENVIRONMENT      = local.environment
  }
  labels = { environment = local.environment, service = "api" }
}

module "cloud_run_worker" {
  source                = "../../modules/cloud-run"
  service_name          = "appetite-engine-worker-${local.environment}"
  project_id            = var.project_id
  region                = var.region
  image                 = "${module.storage.registry_url}/worker:${var.image_tag}"
  min_instances         = 2
  max_instances         = 20
  service_account_email = module.iam.worker_sa_email
  ingress               = "INGRESS_TRAFFIC_INTERNAL_ONLY"
  allow_unauthenticated = false
  invoker_sa_email      = module.iam.pubsub_invoker_sa_email
  vpc_connector_id      = module.networking.vpc_connector_id
  deletion_protection   = true
  resource_limits       = { cpu = "2", memory = "1Gi" }
  env_vars = {
    SPANNER_INSTANCE = module.spanner.instance_name
    SPANNER_DATABASE = module.spanner.database_name
    GCP_PROJECT      = var.project_id
    ENVIRONMENT      = local.environment
  }
  labels = { environment = local.environment, service = "worker" }
}

module "pubsub" {
  source           = "../../modules/pubsub"
  project_id       = var.project_id
  environment      = local.environment
  push_endpoint    = module.cloud_run_worker.service_url
  invoker_sa_email = module.iam.pubsub_invoker_sa_email
  worker_sa_email  = module.iam.worker_sa_email
  api_sa_email     = module.iam.api_sa_email
}

module "monitoring" {
  source                   = "../../modules/monitoring"
  project_id               = var.project_id
  service_name             = module.cloud_run_api.service_name
  service_url              = module.cloud_run_api.service_url
  notification_email       = var.ops_email
  enable_alerts            = true
  worker_service_name      = module.cloud_run_worker.service_name
  worker_service_url       = module.cloud_run_worker.service_url
  pubsub_subscription_name = module.pubsub.dlq_subscription_name
}
