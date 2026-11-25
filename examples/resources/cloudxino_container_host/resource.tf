# A managed container host that runs docker-compose.yml, with one secret and one HTTPS domain.
# Changing compose, env or domains deploys again; changing plan resizes the server in place.
resource "cloudxino_container_host" "apps" {
  name    = "apps"
  plan    = "s-2vcpu-4gb" # data.cloudxino_container_host_plans lists them
  compose = file("${path.module}/docker-compose.yml")

  env = {
    DATABASE_URL = var.database_url
  }

  domains = [
    { domain = "app.example.com", service = "web", port = 8080 },
  ]

  timeouts = {
    create = "45m"
  }
}

# Point the domain's A record here so Let's Encrypt can issue its certificate: the host's own IP,
# or the shared NAT web proxy's IP for a host with ip_type = "nat".
output "ingress_ip" {
  value = cloudxino_container_host.apps.ingress_ip
}

# A NAT host: no public IP (lower price); its domains reach it on 80/443 through the NAT web proxy.
resource "cloudxino_container_host" "nat" {
  name    = "apps-nat"
  plan    = "s-2vcpu-4gb"
  ip_type = "nat"
  compose = file("${path.module}/docker-compose.yml")

  domains = [
    { domain = "shop.example.com", service = "web", port = 8080 },
  ]
}
