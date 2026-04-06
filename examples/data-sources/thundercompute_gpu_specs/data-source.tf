data "thundercompute_gpu_specs" "available" {}

output "gpu_options" {
  value = data.thundercompute_gpu_specs.available.specs
}
