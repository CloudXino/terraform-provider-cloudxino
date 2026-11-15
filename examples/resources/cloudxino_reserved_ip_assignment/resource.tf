# Keep the address when the server is replaced: the reserved IP has no server_id, and the
# assignment moves to the new server.
resource "cloudxino_reserved_ip" "web" {
  region = "dhaka-1"
}

resource "cloudxino_reserved_ip_assignment" "web" {
  reserved_ip_id = cloudxino_reserved_ip.web.id
  server_id      = cloudxino_server.web.id
}
