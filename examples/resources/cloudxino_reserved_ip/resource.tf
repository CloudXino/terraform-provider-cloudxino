# A reserved IP pointed at a server. Changing server_id moves the address; removing it unassigns it.
resource "cloudxino_reserved_ip" "web" {
  region    = "dhaka-1"
  server_id = cloudxino_server.web.id
}

output "web_address" {
  value = cloudxino_reserved_ip.web.ip_address
}
