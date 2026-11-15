package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
)

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"cloudxino": providerserver.NewProtocol6WithError(New("test")()),
}

const edKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOb6dSRqXBbWxkXbBqpB1+jOZGqN2Ztx5u2b8eFbE0a1"

// noChanges: the step after apply plans nothing (no perpetual diff).
var noChanges = resource.ConfigPlanChecks{PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

func TestSSHKey(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				// the comment is dropped by the API: no diff afterwards
				Config: `resource "cloudxino_ssh_key" "k" {
  name       = "laptop"
  public_key = "` + edKey + ` me@laptop\n"
}`,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("cloudxino_ssh_key.k", "id"),
					resource.TestCheckResourceAttr("cloudxino_ssh_key.k", "fingerprint", "SHA256:FbE0a1"),
				),
			},
			{
				Config: `resource "cloudxino_ssh_key" "k" {
  name       = "work-laptop"
  public_key = "` + edKey + ` me@laptop\n"
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("cloudxino_ssh_key.k", plancheck.ResourceActionUpdate),
				}},
				Check: resource.TestCheckResourceAttr("cloudxino_ssh_key.k", "name", "work-laptop"),
			},
			{ResourceName: "cloudxino_ssh_key.k", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"public_key"}},
			{
				Config: `
resource "cloudxino_ssh_key" "k" {
  name       = "work-laptop"
  public_key = "` + edKey + ` me@laptop\n"
}
data "cloudxino_ssh_key" "by_name" {
  name = cloudxino_ssh_key.k.name
}`,
				Check: resource.TestCheckResourceAttrPair("data.cloudxino_ssh_key.by_name", "id", "cloudxino_ssh_key.k", "id"),
			},
		},
	})
}

func TestFirewallNormalizedRulesHaveNoDiff(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: `resource "cloudxino_firewall" "web" {
  name = "web"
  inbound_rule = [
    { protocol = "tcp", ports = "443, 80", sources = ["any"] },
    { protocol = "tcp", ports = "22", sources = ["203.0.113.7/32"] },
    { protocol = "icmp", sources = ["0.0.0.0/0"] },
    { protocol = "udp", sources = ["any"] },
  ]
  tags = ["web"]
}`,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_firewall.web", "inbound_rule.#", "4"),
					resource.TestCheckResourceAttr("cloudxino_firewall.web", "inbound_rule.0.ports", "443, 80"),
					resource.TestCheckNoResourceAttr("cloudxino_firewall.web", "inbound_rule.2.ports"),
					resource.TestCheckResourceAttr("cloudxino_firewall.web", "outbound_rule.#", "1"),
					resource.TestCheckResourceAttr("cloudxino_firewall.web", "status", "active"),
				),
			},
			{
				Config: `resource "cloudxino_firewall" "web" {
  name = "web"
  inbound_rule = [
    { protocol = "tcp", ports = "443", sources = ["0.0.0.0/0"] },
  ]
  outbound_rule = [
    { protocol = "tcp", ports = "all", destinations = ["0.0.0.0/0"] },
    { protocol = "udp", ports = "53", destinations = ["1.1.1.1"] },
  ]
}`,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_firewall.web", "inbound_rule.#", "1"),
					resource.TestCheckResourceAttr("cloudxino_firewall.web", "outbound_rule.#", "2"),
					resource.TestCheckResourceAttr("cloudxino_firewall.web", "tags.#", "0"),
				),
			},
			{ResourceName: "cloudxino_firewall.web", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"inbound_rule", "outbound_rule"}},
		},
	})
}

func TestDNSZoneAndRecord(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "cloudxino_dns_zone" "z" {
  name = "example.com"
}
resource "cloudxino_dns_record" "www" {
  zone_id = cloudxino_dns_zone.z.id
  name    = "www"
  type    = "a"
  content = "203.0.113.10"
  ttl     = 300
}
resource "cloudxino_dns_record" "apex_mx" {
  zone_id  = cloudxino_dns_zone.z.id
  type     = "MX"
  content  = "mail.example.com."
  priority = 10
}`,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_dns_zone.z", "nameservers.#", "2"),
					resource.TestCheckResourceAttr("cloudxino_dns_record.www", "fqdn", "www.example.com"),
					resource.TestCheckResourceAttr("cloudxino_dns_record.www", "type", "a"),
					resource.TestCheckResourceAttr("cloudxino_dns_record.apex_mx", "name", "@"),
					resource.TestCheckResourceAttr("cloudxino_dns_record.apex_mx", "ttl", "3600"),
				),
			},
			{
				ResourceName: "cloudxino_dns_record.www", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraformState) (string, error) {
					rs := s.RootModule().Resources["cloudxino_dns_record.www"]
					return rs.Primary.Attributes["zone_id"] + "/" + rs.Primary.ID, nil
				},
				ImportStateVerifyIgnore: []string{"type"},
			},
			{
				Config: `
resource "cloudxino_dns_zone" "z" {
  name = "example.com"
}
resource "cloudxino_dns_record" "www" {
  zone_id = cloudxino_dns_zone.z.id
  name    = "www"
  type    = "A"
  content = "203.0.113.11"
  ttl     = 300
}
data "cloudxino_dns_zone" "by_name" {
  name = "example.com."
  depends_on = [cloudxino_dns_zone.z]
}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_dns_record.www", "content", "203.0.113.11"),
					resource.TestCheckResourceAttrPair("data.cloudxino_dns_zone.by_name", "id", "cloudxino_dns_zone.z", "id"),
				),
			},
		},
	})
}

func TestServerLifecycle(t *testing.T) {
	newFakeAPI(t)
	cfg := func(name, size, tags string) string {
		return `
resource "cloudxino_ssh_key" "k" {
  name       = "laptop"
  public_key = "` + edKey + `"
}
resource "cloudxino_server" "web" {
  name        = "` + name + `"
  image       = "debian-13"
  size        = "` + size + `"
  ssh_key_ids = [cloudxino_ssh_key.k.id]
  tags        = [` + tags + `]
}`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:           cfg("web-01", "s-1vcpu-1gb", `"web"`),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_server.web", "status", "running"),
					resource.TestCheckResourceAttr("cloudxino_server.web", "ip_address", "203.0.113.20"),
					resource.TestCheckResourceAttr("cloudxino_server.web", "region", "dhaka-1"),
					resource.TestCheckResourceAttr("cloudxino_server.web", "project_id", "p1"),
					resource.TestCheckResourceAttr("cloudxino_server.web", "ip_type", "public"),
					resource.TestCheckResourceAttr("cloudxino_server.web", "ssh_key_ids.#", "1"),
				),
			},
			{
				// rename, retag and resize all happen in place
				Config: cfg("web-02", "s-2vcpu-2gb", `"web", "prod"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_server.web", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_server.web", "name", "web-02"),
					resource.TestCheckResourceAttr("cloudxino_server.web", "cpu", "2"),
					resource.TestCheckResourceAttr("cloudxino_server.web", "tags.#", "2"),
				),
			},
			{
				// another image means a new server
				Config: `
resource "cloudxino_ssh_key" "k" {
  name       = "laptop"
  public_key = "` + edKey + `"
}
resource "cloudxino_server" "web" {
  name        = "web-02"
  image       = "ubuntu-24.04"
  size        = "s-2vcpu-2gb"
  ssh_key_ids = [cloudxino_ssh_key.k.id]
  tags        = ["web", "prod"]
}
data "cloudxino_server" "by_name" {
  name       = "web-02"
  depends_on = [cloudxino_server.web]
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_server.web", plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: resource.TestCheckResourceAttrPair("data.cloudxino_server.by_name", "id", "cloudxino_server.web", "id"),
			},
			{ResourceName: "cloudxino_server.web", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
		},
	})
}

func TestServerCreateError(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: `resource "cloudxino_server" "bad" {
  name  = "x"
  image = "debian-13"
  size  = "nope"
}`,
			ExpectError: regexpMust(`Unknown size 'nope' \(422 validation_error\)`),
		}},
	})
}

func TestLoadBalancer(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: `resource "cloudxino_load_balancer" "lb" {
  name = "web"
  forwarding_rule = [
    { entry_protocol = "http", entry_port = 80, target_port = 8080 },
  ]
  target_ids = ["srv-1"]
}`,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_load_balancer.lb", "ip_address", "203.0.113.50"),
					resource.TestCheckResourceAttr("cloudxino_load_balancer.lb", "forwarding_rule.0.target_protocol", "http"),
					resource.TestCheckResourceAttr("cloudxino_load_balancer.lb", "health_check.protocol", "tcp"),
					resource.TestCheckResourceAttr("cloudxino_load_balancer.lb", "health_check.port", "8080"),
					resource.TestCheckResourceAttr("cloudxino_load_balancer.lb", "algorithm", "round_robin"),
				),
			},
			{
				Config: `resource "cloudxino_load_balancer" "lb" {
  name      = "web"
  algorithm = "least_connections"
  forwarding_rule = [
    { entry_protocol = "http", entry_port = 80, target_port = 8080 },
    { entry_protocol = "tcp", entry_port = 443, target_port = 8443 },
  ]
  health_check = { protocol = "http", path = "/healthz" }
  target_ids   = ["srv-1", "srv-2"]
}`,
				// changed in place, not replaced (project_id left out must not force a new LB)
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_load_balancer.lb", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_load_balancer.lb", "forwarding_rule.#", "2"),
					resource.TestCheckResourceAttr("cloudxino_load_balancer.lb", "health_check.path", "/healthz"),
					resource.TestCheckResourceAttr("cloudxino_load_balancer.lb", "target_ids.#", "2"),
				),
			},
		},
	})
}

func TestAccountAndProject(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config: `
data "cloudxino_account" "me" {}
data "cloudxino_project" "default" {}
data "cloudxino_project" "staging" { name = "Staging" }`,
			Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("data.cloudxino_account.me", "organization_name", "Acme"),
				resource.TestCheckResourceAttr("data.cloudxino_account.me", "api_key_role", "developer"),
				resource.TestCheckResourceAttr("data.cloudxino_project.default", "id", "p1"),
				resource.TestCheckResourceAttr("data.cloudxino_project.staging", "id", "p2"),
			),
		}},
	})
}
