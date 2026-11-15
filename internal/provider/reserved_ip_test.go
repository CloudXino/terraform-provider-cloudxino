package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func twoServers(image string) string {
	return fmt.Sprintf(`
resource "cloudxino_server" "a" {
  name  = "a"
  image = %[1]q
  size  = "s-1vcpu-1gb"
}
resource "cloudxino_server" "b" {
  name  = "b"
  image = "debian-13"
  size  = "s-1vcpu-1gb"
}
`, image)
}

// sameAttr remembers an attribute in step one and checks it is unchanged later (no replacement).
func sameAttr(name, key string, seen *string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		v := rs.Primary.Attributes[key]
		if *seen == "" {
			*seen = v
			return nil
		}
		if v != *seen {
			return fmt.Errorf("%s.%s changed from %s to %s", name, key, *seen, v)
		}
		return nil
	}
}

func TestReservedIPServerID(t *testing.T) {
	f := newFakeAPI(t)
	var addr string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: twoServers("debian-13") + `
resource "cloudxino_reserved_ip" "ip" {
  server_id = cloudxino_server.a.id
}`,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_reserved_ip.ip", "region", "dhaka-1"),
					resource.TestCheckResourceAttr("cloudxino_reserved_ip.ip", "project_id", "p1"),
					resource.TestCheckResourceAttr("cloudxino_reserved_ip.ip", "status", "assigned"),
					resource.TestCheckResourceAttrPair("cloudxino_reserved_ip.ip", "server_id", "cloudxino_server.a", "id"),
					sameAttr("cloudxino_reserved_ip.ip", "ip_address", &addr),
				),
			},
			{
				// move: in place, same address
				Config: twoServers("debian-13") + `
resource "cloudxino_reserved_ip" "ip" {
  server_id = cloudxino_server.b.id
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_reserved_ip.ip", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("cloudxino_reserved_ip.ip", "server_id", "cloudxino_server.b", "id"),
					sameAttr("cloudxino_reserved_ip.ip", "ip_address", &addr),
				),
			},
			{
				// unassigned outside Terraform: the next plan assigns it again
				PreConfig: func() {
					for _, rip := range f.rips {
						rip["server_id"], rip["status"] = nil, "available"
					}
				},
				Config: twoServers("debian-13") + `
resource "cloudxino_reserved_ip" "ip" {
  server_id = cloudxino_server.b.id
}`,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_reserved_ip.ip", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("cloudxino_reserved_ip.ip", "status", "assigned"),
			},
			{
				// server_id removed: unassigned, the address stays
				Config: twoServers("debian-13") + `
resource "cloudxino_reserved_ip" "ip" {}`,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_reserved_ip.ip", "status", "available"),
					resource.TestCheckNoResourceAttr("cloudxino_reserved_ip.ip", "server_id"),
					sameAttr("cloudxino_reserved_ip.ip", "ip_address", &addr),
				),
			},
			{ResourceName: "cloudxino_reserved_ip.ip", ImportState: true, ImportStateVerify: true},
			{
				Config: twoServers("debian-13") + `
resource "cloudxino_reserved_ip" "ip" {}
data "cloudxino_reserved_ip" "by_address" {
  ip_address = cloudxino_reserved_ip.ip.ip_address
}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.cloudxino_reserved_ip.by_address", "id", "cloudxino_reserved_ip.ip", "id"),
					resource.TestCheckResourceAttr("data.cloudxino_reserved_ip.by_address", "status", "available"),
				),
			},
		},
	})
	if len(f.rips) != 0 {
		t.Fatalf("reserved IPs left after destroy: %v", f.rips)
	}
}

func TestReservedIPAssignmentOutlivesServer(t *testing.T) {
	f := newFakeAPI(t)
	var ripID, addr string
	cfg := func(image string) string {
		return twoServers(image) + `
resource "cloudxino_reserved_ip" "ip" {}
resource "cloudxino_reserved_ip_assignment" "a" {
  reserved_ip_id = cloudxino_reserved_ip.ip.id
  server_id      = cloudxino_server.a.id
}`
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:           cfg("debian-13"),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("cloudxino_reserved_ip_assignment.a", "server_id", "cloudxino_server.a", "id"),
					resource.TestCheckResourceAttrPair("cloudxino_reserved_ip_assignment.a", "ip_address", "cloudxino_reserved_ip.ip", "ip_address"),
					// the reserved IP does not track server_id when an assignment does
					resource.TestCheckNoResourceAttr("cloudxino_reserved_ip.ip", "server_id"),
					sameAttr("cloudxino_reserved_ip.ip", "id", &ripID),
					sameAttr("cloudxino_reserved_ip.ip", "ip_address", &addr),
				),
			},
			{
				// a new image replaces the server; the reserved IP stays and moves to the new one
				Config: cfg("ubuntu-24.04"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("cloudxino_server.a", plancheck.ResourceActionDestroyBeforeCreate),
						plancheck.ExpectResourceAction("cloudxino_reserved_ip.ip", plancheck.ResourceActionNoop),
					},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("cloudxino_reserved_ip_assignment.a", "server_id", "cloudxino_server.a", "id"),
					sameAttr("cloudxino_reserved_ip.ip", "id", &ripID),
					sameAttr("cloudxino_reserved_ip.ip", "ip_address", &addr),
				),
			},
			{
				// moved elsewhere outside Terraform: the next apply moves it back
				PreConfig: func() {
					for _, rip := range f.rips {
						for id, s := range f.servers {
							if s["name"] == "b" {
								rip["server_id"] = id
							}
						}
					}
				},
				Config: cfg("ubuntu-24.04"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_reserved_ip_assignment.a", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttrPair("cloudxino_reserved_ip_assignment.a", "server_id", "cloudxino_server.a", "id"),
			},
			{
				// unassigned outside Terraform: the assignment is made again
				PreConfig: func() {
					for _, rip := range f.rips {
						rip["server_id"], rip["status"] = nil, "available"
					}
				},
				Config: cfg("ubuntu-24.04"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_reserved_ip_assignment.a", plancheck.ResourceActionCreate)},
				},
			},
			{ResourceName: "cloudxino_reserved_ip_assignment.a", ImportState: true, ImportStateVerify: true},
			{
				// assignment removed: unassigned, the reserved IP stays
				Config:           twoServers("ubuntu-24.04") + `resource "cloudxino_reserved_ip" "ip" {}`,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					sameAttr("cloudxino_reserved_ip.ip", "id", &ripID),
					func(*terraform.State) error {
						for _, rip := range f.rips {
							if rip["server_id"] != nil {
								return fmt.Errorf("still assigned to %v", rip["server_id"])
							}
						}
						return nil
					},
				),
			},
		},
	})
}

func TestReservedIPUnavailable(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      `resource "cloudxino_reserved_ip" "ip" { region = "full" }`,
			ExpectError: regexpMust(`Reserved IPs are not available right now \(503 unavailable\)`),
		}},
	})
}

func TestReservedIPDataSourceNotFound(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{{
			Config:      `data "cloudxino_reserved_ip" "x" { ip_address = "192.0.2.1" }`,
			ExpectError: regexpMust(`no reserved IP 192.0.2.1`),
		}},
	})
}

func TestReservedIPProbe(t *testing.T) {
	f := newFakeAPI(t)
	f.ripsOff = true
	t.Run("off", func(t *testing.T) {
		skipUnlessReservedIPs(t)
		t.Error("did not skip")
	})
	f.ripsOff = false
	t.Run("on", func(t *testing.T) {
		skipUnlessReservedIPs(t)
	})
	if len(f.rips) != 0 {
		t.Fatalf("probe left a reserved IP: %v", f.rips)
	}
}
