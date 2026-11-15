resource "cloudxino_dns_record" "www" {
  zone_id = cloudxino_dns_zone.example.id
  name    = "www"
  type    = "A"
  content = cloudxino_server.web.ip_address
  ttl     = 300
}

resource "cloudxino_dns_record" "mail" {
  zone_id  = cloudxino_dns_zone.example.id
  name     = "@"
  type     = "MX"
  content  = "mail.example.com"
  priority = 10
}
