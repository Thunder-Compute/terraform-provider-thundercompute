data "thundercompute_gpu_availability" "current" {}

output "gpu_availability" {
  value = data.thundercompute_gpu_availability.current.specs
}
