terraform {
  required_providers {
    cloudxino = {
      source  = "cloudxino/cloudxino"
      version = "~> 0.1"
    }
  }
}

# The API key is read from CLOUDXINO_TOKEN when token is left out.
provider "cloudxino" {
  # token = var.cloudxino_token
}
