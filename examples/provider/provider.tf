terraform {
  required_providers {
    thundercompute = {
      source  = "Thunder-Compute/thundercompute"
      version = "~> 0.2.0"
    }
  }
}

provider "thundercompute" {
  # api_token is read from the TNR_API_TOKEN environment variable by default.
  # Uncomment the line below to set it explicitly:
  # api_token = var.thunder_api_token
}
