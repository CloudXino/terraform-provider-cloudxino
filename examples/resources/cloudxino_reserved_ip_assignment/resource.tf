# Keep the address when the server is replaced: the reserved IP has no server_id, and the
# assignment moves to the new server.
resource "cloudxino_reserved_ip" "web" {
  region = "dhaka-1" # the server must be in this region
}

resource "cloudxino_reserved_ip_assignment" "web" {
  reserved_ip_id = cloudxino_reserved_ip.web.id
  server_id      = cloudxino_server.web.id
}

# Or point it at a CC2 load balancer (exactly one of server_id, load_balancer_id).
resource "cloudxino_reserved_ip_assignment" "lb" {
  reserved_ip_id   = cloudxino_reserved_ip.lb.id
  load_balancer_id = cloudxino_load_balancer.web.id
}
