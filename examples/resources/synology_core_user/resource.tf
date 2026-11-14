variable "backup_password" {
  type      = string
  sensitive = true
  ephemeral = true
}

resource "synology_core_user" "backup" {
  name                = "mininuke-backup"
  description         = "Nightly backups from mininuke (SMB only)"
  groups              = ["users"]
  password_wo         = var.backup_password
  password_wo_version = 1
}
