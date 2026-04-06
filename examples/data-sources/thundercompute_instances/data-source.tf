data "thundercompute_instances" "all" {}

output "running_instances" {
  value = data.thundercompute_instances.all.instances
}
