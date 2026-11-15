# A small web stack: two servers behind a load balancer, a firewall and DNS.
#
#   export CLOUDXINO_TOKEN=cxk_...
#   terraform init && terraform apply

terraform {
  required_providers {
    cloudxino = {
      source  = "cloudxino/cloudxino"
      version = "~> 0.1"
    }
  }
}

provider "cloudxino" {}

variable "domain" {
  type = string
}

resource "cloudxino_ssh_key" "me" {
  name       = "deploy"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "cloudxino_server" "web" {
  count       = 2
  name        = "web-${count.index + 1}"
  image       = "debian-13"
  size        = "s-1vcpu-1gb"
  ssh_key_ids = [cloudxino_ssh_key.me.id]
  tags        = ["web"]
}

resource "cloudxino_firewall" "web" {
  name = "web"
  inbound_rule = [
    { protocol = "tcp", ports = "80,443", sources = ["0.0.0.0/0"] },
    { protocol = "tcp", ports = "22", sources = ["0.0.0.0/0"] },
  ]
  tags = ["web"]
}

resource "cloudxino_load_balancer" "web" {
  name = "web"
  forwarding_rule = [
    { entry_protocol = "http", entry_port = 80, target_port = 80 },
  ]
  target_ids = cloudxino_server.web[*].id
}

resource "cloudxino_dns_zone" "site" {
  name = var.domain
}

resource "cloudxino_dns_record" "root" {
  zone_id = cloudxino_dns_zone.site.id
  name    = "@"
  type    = "A"
  content = cloudxino_load_balancer.web.ip_address
}

output "nameservers" {
  value = cloudxino_dns_zone.site.nameservers
}

output "load_balancer_ip" {
  value = cloudxino_load_balancer.web.ip_address
}
