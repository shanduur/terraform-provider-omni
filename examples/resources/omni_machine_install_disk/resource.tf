# Pin the install disk of a machine by its dev path.
resource "omni_machine_install_disk" "by_path" {
  machine_id = "392102d6-6954-4f9b-a65d-896af85bca09"
  disk       = "/dev/sda"
}

# Or let Omni pick the first disk matching a CEL expression, evaluated against each disk of the
# machine in the Talos disk locator environment.
resource "omni_machine_install_disk" "by_selector" {
  machine_id    = "e8b8e0f0-1111-2222-3333-444455556666"
  disk_selector = "disk.transport == 'nvme'"
}
