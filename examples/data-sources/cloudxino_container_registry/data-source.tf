# A Container Hub registry made in the console. Push images to <endpoint>/<image>:<tag>, then let a
# container host pull them with registry_ids.
data "cloudxino_container_registry" "hub" {
  name = "my-hub"
}

resource "cloudxino_container_host" "apps" {
  name         = "apps"
  plan         = "s-2vcpu-4gb"
  registry_ids = [data.cloudxino_container_registry.hub.id]
  compose      = <<-EOT
    services:
      web:
        image: ${data.cloudxino_container_registry.hub.endpoint}/web:1.0
  EOT
}
