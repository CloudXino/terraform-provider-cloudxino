## Unreleased

Features:

- `cloudxino_reserved_ip` resource and data source: a public IPv4 the team keeps, pointed at a CC2
  server with `server_id` (changing it moves the address, removing it unassigns it). Import by ID;
  the data source finds one by `ip_address`.
- `cloudxino_reserved_ip_assignment` resource: points a reserved IP at a server on its own, so the
  address outlives a `cloudxino_server` that is replaced.

Needs reserved IPs on the API (503 `unavailable` until then). Depends on cloudxino-go with the
`ReservedIPs` service.

## 0.1.1 (November 15, 2026)

Fixes:

- The default API address is now `https://cloudxino.com/api` (cloudxino-go v0.1.1). 0.1.0 pointed at
  `console.cloudxino.com`, which does not resolve, so it only worked with `api_url` or `CLOUDXINO_API_URL` set.
- `cloudxino_load_balancer`: a load balancer without a `health_check` block no longer plans a replacement
  right after it is created, and changes such as `algorithm` are made in place instead of recreating it.
  `health_check.port` is now computed when left out, and `project_id` is no longer computed.

Tested against the real API with the new `acceptance` workflow.

## 0.1.0 (November 15, 2026)

First release.

Resources: `cloudxino_server`, `cloudxino_firewall`, `cloudxino_load_balancer`, `cloudxino_dns_zone`,
`cloudxino_dns_record`, `cloudxino_ssh_key`.

Data sources: `cloudxino_account`, `cloudxino_project`, `cloudxino_server`, `cloudxino_dns_zone`,
`cloudxino_ssh_key`.
