# Terraform provider for CloudXino

Manage [CloudXino](https://cloudxino.com) resources with Terraform or OpenTofu: CC2 servers, Cloud
Firewalls, load balancers, reserved IPs, Kubernetes clusters, managed container hosts, hosted DNS and
SSH keys. Built on the
[CloudXino Go SDK](https://github.com/cloudxino/cloudxino-go).

```hcl
terraform {
  required_providers {
    cloudxino = {
      source  = "cloudxino/cloudxino"
      version = "~> 0.1"
    }
  }
}

provider "cloudxino" {} # reads CLOUDXINO_TOKEN

resource "cloudxino_ssh_key" "me" {
  name       = "laptop"
  public_key = file("~/.ssh/id_ed25519.pub")
}

resource "cloudxino_server" "web" {
  name        = "web-01"
  image       = "debian-13"
  size        = "s-1vcpu-1gb"
  ssh_key_ids = [cloudxino_ssh_key.me.id]
  tags        = ["web"]
}
```

Make an API key in the console under **Account → API keys** and export it as `CLOUDXINO_TOKEN`.
A key with the `viewer` role can only read; creating servers needs `developer` or `admin`, and
servers are billed from the team's wallet.

Full documentation is in [docs/](docs/) and on the Terraform Registry once released;
[examples/complete](examples/complete) is a small web stack (servers, load balancer, firewall, DNS).

## Resources and data sources

| Resource | Data source | Notes |
| --- | --- | --- |
| `cloudxino_server` | `cloudxino_server` | `size` changes resize in place; other changes replace the server |
| `cloudxino_firewall` | | applies by `server_ids` and by `tags` |
| `cloudxino_load_balancer` | | |
| `cloudxino_dns_zone` | `cloudxino_dns_zone` | |
| `cloudxino_dns_record` | | import with `<zone_id>/<record_id>` |
| `cloudxino_ssh_key` | `cloudxino_ssh_key` | public keys only |
| `cloudxino_reserved_ip` | `cloudxino_reserved_ip` | a public IPv4 the team keeps; optional `server_id`; data source by `ip_address` |
| `cloudxino_reserved_ip_assignment` | | points a reserved IP at a server, so the address outlives a replaced server |
| `cloudxino_kubernetes_cluster` | `cloudxino_kubernetes_cluster` | CKS: one node pool (`node_size`, `node_count`); sensitive `kubeconfig`; `name`, `project_id` and `node_count` change in place; data source by `name` |
| `cloudxino_container_host` | `cloudxino_container_host` | Managed Container Service: `compose`, sensitive `env` and `domains` deploy and wait; `plan` resizes in place; `name`, `region` and `project_id` replace the host |
| | `cloudxino_container_host_plans` | the MCS plans with monthly prices |
| | `cloudxino_project` | the default project, or one by `id` or `name` |
| | `cloudxino_account` | the team and role of the API key |

Every resource can be imported with `terraform import`.

## How it behaves

- Creates send an `Idempotency-Key`, so a create the provider retries after a network error never
  makes a second resource.
- Rate limits (429), maintenance (503) and gateway errors are retried with backoff, honouring
  `Retry-After`. A 503 `unavailable` (no free reserved IP) fails at once.
- `cloudxino_server`, `cloudxino_load_balancer`, `cloudxino_kubernetes_cluster` and
  `cloudxino_container_host` wait until the resource is ready (or gone) and take `timeouts`. A
  container host also waits for each deployment it makes. A cluster is polled every 15s, the API's limit
  for reading one cluster. A server whose create fails while waiting is kept in state as tainted, so the
  next apply replaces it instead of leaving it unmanaged.
- The API normalises firewall rules (`"443, 80"` becomes `"80,443"`, `any` becomes `0.0.0.0/0`);
  the provider keeps what you wrote when it means the same thing, so there is no perpetual diff.

## Developing

Requires Go (see `go.mod`) and Terraform 1.x or OpenTofu.

```sh
go build ./...
go test ./...      # runs real Terraform against an in-memory fake API; creates nothing
go generate ./...  # regenerates docs/ from the schema and examples/
```

To try a local build, point Terraform at it with `dev_overrides` in `~/.terraformrc`:

```hcl
provider_installation {
  dev_overrides { "cloudxino/cloudxino" = "/path/to/go/bin" }
  direct {}
}
```

Releases are made by pushing a `v*` tag; GoReleaser builds and signs them for the registry.

## License

[MPL-2.0](LICENSE)
