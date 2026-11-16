## 0.1.4 (unreleased)

Features:

- `cloudxino_kubernetes_cluster` resource and data source: a CKS (managed Kubernetes) cluster with
  its default node pool (`node_size`, `node_count`), `region`, `version`, `ingress`, `vpc_id` and
  `project_id`; computed `status`, `endpoint`, `node_pool_id` and a sensitive `kubeconfig` (read
  while the cluster is active, kept in state). `name`, `project_id` and `node_count` change in place
  (the pool is resized and the cluster waited for); other changes replace the cluster. Import by ID;
  the data source finds one by `name` or `id`. Example: `examples/kubernetes`.

Uses cloudxino-go `Clusters` (pseudo-version until cloudxino-go v0.1.4 is tagged).

## 0.1.3 (unreleased)

Features:

- `cloudxino_reserved_ip_assignment`: `load_balancer_id` points a reserved IP at a CC2 load balancer
  (exactly one of `server_id`, `load_balancer_id`; `server_id` is now optional). Moving between a
  server and a load balancer is an in-place update. A CKS LoadBalancer Service asks for a reserved
  IP with its own annotation `cloudxino.com/reserved-ip`.

Uses cloudxino-go v0.1.3 (`ReservedIPs.AssignLoadBalancer`).

## 0.1.2 (November 16, 2026)

Features:

- `cloudxino_reserved_ip` resource and data source: a public IPv4 the team keeps, pointed at a CC2
  server with `server_id` (changing it moves the address, removing it unassigns it). Import by ID;
  the data source finds one by `ip_address`.
- `cloudxino_reserved_ip_assignment` resource: points a reserved IP at a server on its own, so the
  address outlives a `cloudxino_server` that is replaced.

Needs reserved IPs switched on for the API (503 `unavailable` until then). Uses cloudxino-go v0.1.2
(the `ReservedIPs` service).

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
