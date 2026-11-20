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

# Point the domain's A record at the host so Let's Encrypt can issue its certificate.
output "ip_address" {
  value = cloudxino_container_host.apps.ip_address
}
