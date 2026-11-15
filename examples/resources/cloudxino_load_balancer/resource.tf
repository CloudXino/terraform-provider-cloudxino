resource "cloudxino_load_balancer" "web" {
  name = "web"

  forwarding_rule = [
    { entry_protocol = "http", entry_port = 80, target_port = 8080 },
    { entry_protocol = "tcp", entry_port = 443, target_port = 8443 },
  ]

  health_check = {
    protocol = "http"
    path     = "/healthz"
  }

  target_ids = cloudxino_server.web[*].id
}
