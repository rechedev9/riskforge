terraform {
  backend "gcs" {
    bucket = "YOUR_STATE_BUCKET_NAME"
    prefix = "environments/dev"
  }
}
