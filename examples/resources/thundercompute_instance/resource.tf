resource "thundercompute_instance" "training" {
  gpu_type     = "H100"
  mode         = "prototyping"
  template     = "base"
  cpu_cores    = 4
  disk_size_gb = 200
  num_gpus     = 1
  http_ports   = [8888]

  allow_snapshot_modify = true
}

output "ssh_command" {
  value = "ssh -p ${thundercompute_instance.training.port} ubuntu@${thundercompute_instance.training.ip}"
}
