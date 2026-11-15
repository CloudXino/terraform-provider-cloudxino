resource "cloudxino_server" "web" {
  name        = "web-01"
  image       = "debian-13"
  size        = "s-1vcpu-1gb"
  ssh_key_ids = [cloudxino_ssh_key.laptop.id]
  tags        = ["web"]

  timeouts = {
    create = "20m"
  }
}
