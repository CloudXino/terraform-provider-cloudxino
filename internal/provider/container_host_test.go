package provider

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

const goodCompose = "services:\n  web:\n    image: nginx:1.27\n"

func containerHostConfig(name, plan, compose, dbPassword string) string {
	return fmt.Sprintf(`
resource "cloudxino_container_host" "h" {
  name    = %q
  plan    = %q
  compose = %q
  env = {
    DB_PASSWORD = %q
  }
  domains = [
    { domain = "app.example.com", service = "web", port = 80 },
  ]
  timeouts = { create = "45m" }
}
`, name, plan, compose, dbPassword)
}

func deploymentsOf(f *fakeAPI) []map[string]any {
	for _, deps := range f.mcs.deployments {
		return deps
	}
	return nil
}

func checkDeployments(f *fakeAPI, want int) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := len(deploymentsOf(f)); got != want {
			return fmt.Errorf("%d deployments, want %d", got, want)
		}
		return nil
	}
}

func TestContainerHost(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				// created, then compose, env and domains go out in one deployment
				Config:           containerHostConfig("apps", "s-1vcpu-2gb", goodCompose, "s3cret"),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "status", "ready"),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "region", "dhaka-1"),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "project_id", "p1"),
					resource.TestMatchResourceAttr("cloudxino_container_host.h", "ip_address", regexp.MustCompile(`^203\.0\.113\.\d+$`)),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "ip_type", "public"),
					resource.TestCheckResourceAttrPair("cloudxino_container_host.h", "ingress_ip", "cloudxino_container_host.h", "ip_address"),
					resource.TestMatchResourceAttr("cloudxino_container_host.h", "portainer_url", regexp.MustCompile(`^https://203-0-113-\d+\.sslip\.io$`)),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "last_deployment.number", "1"),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "last_deployment.status", "succeeded"),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "domains.0.domain", "app.example.com"),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "env.DB_PASSWORD", "s3cret"),
					sameAttr("cloudxino_container_host.h", "id", &id),
					checkDeployments(f, 1),
					func(*terraform.State) error {
						dep := deploymentsOf(f)[0]
						if dep["note"] != deploymentNote || dep["compose"] != goodCompose || len(dep["env_keys"].([]string)) != 1 {
							return fmt.Errorf("deployment %v", dep)
						}
						return nil
					},
				),
			},
			{
				// a new secret: one more deployment, same host
				Config: containerHostConfig("apps", "s-1vcpu-2gb", goodCompose, "n3w"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_container_host.h", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					sameAttr("cloudxino_container_host.h", "id", &id),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "last_deployment.number", "2"),
					checkDeployments(f, 2),
					func(*terraform.State) error {
						for _, env := range f.mcs.env {
							if env["DB_PASSWORD"] != "n3w" {
								return fmt.Errorf("env %v", env)
							}
						}
						return nil
					},
				),
			},
			{
				// another plan: resized in place, nothing deployed
				Config: containerHostConfig("apps", "s-2vcpu-4gb", goodCompose, "n3w"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_container_host.h", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					sameAttr("cloudxino_container_host.h", "id", &id),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "plan", "s-2vcpu-4gb"),
					checkDeployments(f, 2),
				),
			},
			{
				// a deployment that fails: the apply fails with the end of the compose output
				Config:      containerHostConfig("apps", "s-2vcpu-4gb", "services:\n  web:\n    image: nope\n", "n3w"),
				ExpectError: regexp.MustCompile(`pull access denied for nope`),
			},
			{
				// the fixed file deploys again on the same host
				Config:           containerHostConfig("apps", "s-2vcpu-4gb", goodCompose, "n3w"),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					sameAttr("cloudxino_container_host.h", "id", &id),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "last_deployment.number", "4"),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "last_deployment.status", "succeeded"),
				),
			},
			{
				ResourceName: "cloudxino_container_host.h", ImportState: true, ImportStateVerify: true,
				// an imported host does not know which of these Terraform should manage
				ImportStateVerifyIgnore: []string{"timeouts", "compose", "env", "domains"},
			},
			{
				Config: containerHostConfig("apps", "s-2vcpu-4gb", goodCompose, "n3w") + `
data "cloudxino_container_host" "by_name" {
  name = cloudxino_container_host.h.name
}

data "cloudxino_container_host_plans" "all" {}
`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.cloudxino_container_host.by_name", "id", "cloudxino_container_host.h", "id"),
					resource.TestCheckResourceAttrPair("data.cloudxino_container_host.by_name", "ip_address", "cloudxino_container_host.h", "ip_address"),
					resource.TestCheckResourceAttr("data.cloudxino_container_host.by_name", "compose", goodCompose),
					resource.TestCheckResourceAttr("data.cloudxino_container_host.by_name", "env_keys.0", "DB_PASSWORD"),
					resource.TestCheckResourceAttr("data.cloudxino_container_host.by_name", "domains.0.port", "80"),
					resource.TestCheckResourceAttr("data.cloudxino_container_host.by_name", "last_deployment.number", "4"),
					resource.TestCheckResourceAttr("data.cloudxino_container_host_plans.all", "enabled", "true"),
					resource.TestCheckResourceAttr("data.cloudxino_container_host_plans.all", "plans.#", "2"),
					resource.TestCheckResourceAttr("data.cloudxino_container_host_plans.all", "plans.0.slug", "s-1vcpu-2gb"),
					resource.TestCheckResourceAttr("data.cloudxino_container_host_plans.all", "plans.0.per_month", "1299"),
				),
			},
			{
				// another name: a new host
				Config: containerHostConfig("apps-2", "s-2vcpu-4gb", goodCompose, "n3w"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_container_host.h", plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: resource.TestCheckResourceAttr("cloudxino_container_host.h", "name", "apps-2"),
			},
		},
	})
	if len(f.mcs.hosts) != 0 {
		t.Fatalf("hosts left after destroy: %v", f.mcs.hosts)
	}
}

// Without compose in the configuration, deployments made elsewhere (cxctl from CI) are left alone;
// an env change then deploys the saved compose file.
func TestContainerHostComposeManagedElsewhere(t *testing.T) {
	f := newFakeAPI(t)
	config := func(env string) string {
		return fmt.Sprintf(`
resource "cloudxino_container_host" "h" {
  name = "ci-apps"
  plan = "s-1vcpu-2gb"
  env  = { MODE = %q }
}
`, env)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				// no compose file yet: the env vars are only saved
				Config:           config("a"),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckNoResourceAttr("cloudxino_container_host.h", "last_deployment"),
					checkDeployments(f, 0),
				),
			},
			{
				// CI deployed a compose file: no diff
				PreConfig: func() {
					for id, h := range f.mcs.hosts {
						h["compose"] = goodCompose
						f.mcs.deployments[id] = append(f.mcs.deployments[id], map[string]any{"id": "dep-ci", "number": 1, "status": "succeeded",
							"compose": goodCompose, "env_keys": []string{"MODE"}, "domains": []any{}})
					}
				},
				Config:           config("a"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            resource.TestCheckResourceAttr("cloudxino_container_host.h", "last_deployment.id", "dep-ci"),
			},
			{
				Config:           config("b"),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					checkDeployments(f, 2),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "last_deployment.number", "2"),
					func(*terraform.State) error {
						if dep := deploymentsOf(f)[1]; dep["compose"] != goodCompose {
							return fmt.Errorf("deployed %v", dep["compose"])
						}
						return nil
					},
				),
			},
		},
	})
}

func TestContainerHostDriftAndGone(t *testing.T) {
	f := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: containerHostConfig("apps", "s-1vcpu-2gb", goodCompose, "s3cret")},
			{
				// someone changed the env var and the domains in the console: the next apply puts them back
				PreConfig: func() {
					for id, h := range f.mcs.hosts {
						f.mcs.env[id]["DB_PASSWORD"] = "changed"
						h["domains"] = []any{}
					}
				},
				Config:           containerHostConfig("apps", "s-1vcpu-2gb", goodCompose, "s3cret"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_container_host.h", plancheck.ResourceActionUpdate)}},
				Check: resource.ComposeTestCheckFunc(
					checkDeployments(f, 2),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "domains.#", "1"),
				),
			},
			{
				PreConfig: func() {
					for id := range f.mcs.hosts {
						delete(f.mcs.hosts, id)
					}
				},
				Config: containerHostConfig("apps", "s-1vcpu-2gb", goodCompose, "s3cret"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_container_host.h", plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}

func TestContainerHostErrors(t *testing.T) {
	f := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      containerHostConfig("apps", "s-64vcpu", goodCompose, "x"),
				ExpectError: regexp.MustCompile(`Unknown plan 's-64vcpu'`),
			},
			{
				Config:      strings.Replace(containerHostConfig("apps", "s-1vcpu-2gb", goodCompose, "x"), `"app.example.com"`, `"App.Example.com."`, 1),
				ExpectError: regexp.MustCompile(`write "App.Example.com." as "app.example.com"`),
			},
			{
				// the install fails: the apply fails with the reason and the host stays in state (tainted)
				PreConfig:   func() { f.mcs.failInstall = true },
				Config:      containerHostConfig("apps", "s-1vcpu-2gb", goodCompose, "x"),
				ExpectError: regexp.MustCompile(`(?s)exit 100.*docker-ce`),
			},
			{
				PreConfig: func() { f.mcs.failInstall = false },
				Config:    containerHostConfig("apps", "s-1vcpu-2gb", goodCompose, "x"),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_container_host.h", plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: resource.TestCheckResourceAttr("cloudxino_container_host.h", "status", "ready"),
			},
		},
	})
}

func TestContainerHostDataSourceNeedsOneMatch(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      `data "cloudxino_container_host" "x" { name = "missing" }`,
				ExpectError: regexp.MustCompile(`0 container hosts named "missing"`),
			},
		},
	})
}

// registry_ids: chosen by the registry data source, saved before the deployment, and a change
// deploys again; [] removes the logins.
func TestContainerHostRegistries(t *testing.T) {
	f := newFakeAPI(t)
	config := func(ids string) string {
		return fmt.Sprintf(`
data "cloudxino_container_registry" "hub" { name = "ai-platform" }

resource "cloudxino_container_host" "h" {
  name         = "apps"
  plan         = "s-1vcpu-2gb"
  compose      = %q
  registry_ids = %s
}
`, goodCompose, ids)
	}
	hostRegistries := func(want ...string) resource.TestCheckFunc {
		return func(*terraform.State) error {
			for _, h := range f.mcs.hosts {
				if got := fmt.Sprint(h["registry_ids"]); got != fmt.Sprint(want) {
					return fmt.Errorf("host registry_ids %s, want %v", got, want)
				}
			}
			return nil
		}
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:           config("[data.cloudxino_container_registry.hub.id]"),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("data.cloudxino_container_registry.hub", "id", "reg-1"),
					resource.TestCheckResourceAttr("data.cloudxino_container_registry.hub", "endpoint", "cch.example.com/ai-platform-x"),
					resource.TestCheckResourceAttr("data.cloudxino_container_registry.hub", "status", "active"),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "registry_ids.#", "1"),
					resource.TestCheckTypeSetElemAttr("cloudxino_container_host.h", "registry_ids.*", "reg-1"),
					hostRegistries("reg-1"),
					checkDeployments(f, 1),
				),
			},
			{
				// no change: no new deployment
				Config:           config("[data.cloudxino_container_registry.hub.id]"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}},
				Check:            checkDeployments(f, 1),
			},
			{
				Config:           config(`[data.cloudxino_container_registry.hub.id, "reg-2"]`),
				ConfigPlanChecks: noChanges,
				Check:            resource.ComposeTestCheckFunc(hostRegistries("reg-1", "reg-2"), checkDeployments(f, 2)),
			},
			{
				Config:           config("[]"),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "registry_ids.#", "0"),
					hostRegistries(), checkDeployments(f, 3),
				),
			},
			{
				Config:      config(`["nope"]`),
				ExpectError: regexp.MustCompile(`active Container Hub registries`),
			},
		},
	})
}

func TestContainerRegistryDataSource(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config: `data "cloudxino_container_registry" "x" { id = "reg-2" }`,
				Check:  resource.TestCheckResourceAttr("data.cloudxino_container_registry.x", "name", "other"),
			},
			{
				Config:      `data "cloudxino_container_registry" "x" { name = "missing" }`,
				ExpectError: regexp.MustCompile(`not found`),
			},
			{
				Config:      `data "cloudxino_container_registry" "x" {}`,
				ExpectError: regexp.MustCompile(`Set id or name`),
			},
		},
	})
}

func natHostConfig(ipType string) string {
	return fmt.Sprintf(`
resource "cloudxino_container_host" "h" {
  name    = "apps"
  plan    = "s-1vcpu-2gb"
  ip_type = %q
  compose = %q
  domains = [
    { domain = "app.example.com", service = "web", port = 80 },
  ]
}

data "cloudxino_container_host" "by_id" {
  id = cloudxino_container_host.h.id
}
`, ipType, goodCompose)
}

func TestContainerHostNat(t *testing.T) {
	f := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:           natHostConfig("nat"),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "ip_type", "nat"),
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "ingress_ip", "103.143.148.30"),
					resource.TestMatchResourceAttr("cloudxino_container_host.h", "ip_address", regexp.MustCompile(`^172\.16\.0\.\d+$`)),
					resource.TestMatchResourceAttr("cloudxino_container_host.h", "portainer_url", regexp.MustCompile(`^https://mcs-.+\.103-143-148-30\.sslip\.io$`)),
					resource.TestCheckResourceAttr("data.cloudxino_container_host.by_id", "ip_type", "nat"),
					resource.TestCheckResourceAttr("data.cloudxino_container_host.by_id", "ingress_ip", "103.143.148.30"),
					func(*terraform.State) error {
						for _, h := range f.mcs.hosts {
							if h["ip_type"] != "nat" {
								return fmt.Errorf("created with ip_type %v", h["ip_type"])
							}
						}
						return nil
					},
				),
			},
			{
				// the IP type is fixed at creation: a change replaces the host
				Config: natHostConfig("public"),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("cloudxino_container_host.h", plancheck.ResourceActionReplace),
				}},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_container_host.h", "ip_type", "public"),
					resource.TestMatchResourceAttr("cloudxino_container_host.h", "ingress_ip", regexp.MustCompile(`^203\.0\.113\.\d+$`)),
				),
			},
		},
	})
}
