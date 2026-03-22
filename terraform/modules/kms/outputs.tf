output "key_ring_name" {
  value       = var.enable_cmek ? google_kms_key_ring.main[0].name : ""
  description = "KMS key ring name. Empty string when enable_cmek = false."
}

output "crypto_key_id" {
  value       = var.enable_cmek ? google_kms_crypto_key.main[0].id : ""
  description = "KMS crypto key self-link. Empty string when enable_cmek = false."
}
