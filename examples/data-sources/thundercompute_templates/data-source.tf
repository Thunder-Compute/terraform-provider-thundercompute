data "thundercompute_templates" "available" {}

output "available_templates" {
  value = data.thundercompute_templates.available.templates
}
