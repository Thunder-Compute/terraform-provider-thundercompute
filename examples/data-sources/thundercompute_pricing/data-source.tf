data "thundercompute_pricing" "current" {}

output "hourly_prices" {
  value = data.thundercompute_pricing.current.pricing
}
