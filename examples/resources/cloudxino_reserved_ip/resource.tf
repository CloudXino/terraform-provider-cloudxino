# A reserved IP pointed at a server. Changing server_id moves the address; removing it unassigns it.
resource "cloudxino_reserved_ip" "web" {
  region    = cloudxino_server.web.region # the server must be in the same region
  server_id = cloudxino_server.web.id
}

output "web_address" {
  value = cloudxino_reserved_ip.web.ip_address
}
