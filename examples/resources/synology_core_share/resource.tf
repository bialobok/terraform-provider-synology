resource "synology_core_share" "backups" {
  name        = "mininuke-backups"
  vol_path    = "/volume1"
  description = "Encrypted backups from mininuke"

  # Destroying a share deletes its files.
  lifecycle {
    prevent_destroy = true
  }
}
