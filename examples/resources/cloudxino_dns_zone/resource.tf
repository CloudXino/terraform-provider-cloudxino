resource "cloudxino_dns_zone" "example" {
  name = "example.com"
}

# Set these at the domain's registrar.
output "nameservers" {
  value = cloudxino_dns_zone.example.nameservers
}
