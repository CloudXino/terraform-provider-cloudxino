# A Managed Container Service host running docker-compose.yml, served over HTTPS on a domain in a
# CloudXino DNS zone.
#
#   export CLOUDXINO_TOKEN=cxk_...
#   terraform init && terraform apply -var domain=app.example.com -var zone=example.com   # about 5 minutes
#
# Edit docker-compose.yml and apply again to deploy it. To deploy from CI instead, leave compose
# out here and run `cxctl containers deploy demo -f docker-compose.yml --wait` in the pipeline.

terraform {
  required_providers {
    cloudxino = {
      source  = "cloudxino/cloudxino"
      version = "~> 0.1"
    }
  }
}

provider "cloudxino" {}

variable "zone" {
  type        = string
  description = "A DNS zone hosted on CloudXino, e.g. example.com"
}

variable "domain" {
  type        = string
  description = "The app's domain in that zone, e.g. app.example.com"
}

variable "greeting" {
  type    = string
  default = "Hello from CloudXino"
}

data "cloudxino_container_host_plans" "all" {}

resource "cloudxino_container_host" "demo" {
  name    = "demo"
  plan    = data.cloudxino_container_host_plans.all.plans[0].slug # the cheapest
  compose = file("${path.module}/docker-compose.yml")

  env = {
    GREETING = var.greeting
  }

  domains = [
    { domain = var.domain, service = "web", port = 80 },
  ]
}

data "cloudxino_dns_zone" "zone" {
  name = var.zone
}

# Let's Encrypt issues the certificate once this record points at the host.
resource "cloudxino_dns_record" "app" {
  zone_id = data.cloudxino_dns_zone.zone.id
  type    = "A"
  name    = trimsuffix(var.domain, ".${var.zone}")
  content = cloudxino_container_host.demo.ip_address
  ttl     = 300
}

output "url" {
  value = "https://${var.domain}"
}

output "portainer_url" {
  value = cloudxino_container_host.demo.portainer_url
}
