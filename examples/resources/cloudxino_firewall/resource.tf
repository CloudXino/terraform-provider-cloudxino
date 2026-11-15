resource "cloudxino_firewall" "web" {
  name = "web"

  inbound_rule = [
    { protocol = "tcp", ports = "80,443", sources = ["0.0.0.0/0"] },
    { protocol = "tcp", ports = "22", sources = ["203.0.113.7/32"] },
    { protocol = "icmp", sources = ["0.0.0.0/0"] },
  ]

  # Left out: all outbound traffic is allowed.
  # outbound_rule = [{ protocol = "tcp", ports = "all", destinations = ["0.0.0.0/0"] }]

  # Every server with one of these tags gets the firewall.
  tags = ["web"]
}
