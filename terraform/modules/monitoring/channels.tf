resource "google_monitoring_notification_channel" "email" {
  display_name = "Ops Team Email"
  type         = "email"
  project      = var.project_id

  labels = {
    email_address = var.notification_email
  }
}
