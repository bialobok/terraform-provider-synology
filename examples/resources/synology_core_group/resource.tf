resource "synology_core_group" "backup_writers" {
  name        = "backup-writers"
  description = "Machines that write backups to the NAS"
}
