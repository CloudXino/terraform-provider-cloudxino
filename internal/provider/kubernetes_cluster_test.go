package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func clusterConfig(name, size string, count int) string {
	return fmt.Sprintf(`
resource "cloudxino_kubernetes_cluster" "k" {
  name       = %q
  node_size  = %q
  node_count = %d
  timeouts   = { create = "40m" }
}

output "kubeconfig" {
  value     = cloudxino_kubernetes_cluster.k.kubeconfig
  sensitive = true
}
`, name, size, count)
}

func TestKubernetesCluster(t *testing.T) {
	f := newFakeAPI(t)
	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:           clusterConfig("prod", "k8s-2vcpu-4gb", 2),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "status", "active"),
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "region", "dhaka-1"),
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "version", "1.36.5"),
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "project_id", "p1"),
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "ingress", "true"),
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "node_count", "2"),
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "endpoint", "https://k8s.example.com:6401"),
					resource.TestCheckResourceAttrSet("cloudxino_kubernetes_cluster.k", "node_pool_id"),
					resource.TestMatchResourceAttr("cloudxino_kubernetes_cluster.k", "kubeconfig", regexp.MustCompile(`(?s)kind: Config.*- name: prod`)),
					resource.TestMatchOutput("kubeconfig", regexp.MustCompile(`server: https://k8s.example.com:6401`)),
					sameAttr("cloudxino_kubernetes_cluster.k", "id", &id),
				),
			},
			{
				// rename and resize: in place, the cluster stays
				Config: clusterConfig("prod-2", "k8s-2vcpu-4gb", 3),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply:             []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_kubernetes_cluster.k", plancheck.ResourceActionUpdate)},
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "name", "prod-2"),
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "node_count", "3"),
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "status", "active"),
					sameAttr("cloudxino_kubernetes_cluster.k", "id", &id),
					func(*terraform.State) error {
						for _, p := range f.pools {
							if p["count"] != 3.0 {
								return fmt.Errorf("pool count %v", p["count"])
							}
						}
						return nil
					},
				),
			},
			{
				// resized outside Terraform: the next apply sizes it back
				PreConfig: func() {
					for _, p := range f.pools {
						p["count"] = 5.0
					}
				},
				Config: clusterConfig("prod-2", "k8s-2vcpu-4gb", 3),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_kubernetes_cluster.k", plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "node_count", "3"),
			},
			{ResourceName: "cloudxino_kubernetes_cluster.k", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
			{
				Config: clusterConfig("prod-2", "k8s-2vcpu-4gb", 3) + `
data "cloudxino_kubernetes_cluster" "by_name" {
  name = cloudxino_kubernetes_cluster.k.name
}`,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("data.cloudxino_kubernetes_cluster.by_name", "id", "cloudxino_kubernetes_cluster.k", "id"),
					resource.TestCheckResourceAttrPair("data.cloudxino_kubernetes_cluster.by_name", "kubeconfig", "cloudxino_kubernetes_cluster.k", "kubeconfig"),
					resource.TestCheckResourceAttr("data.cloudxino_kubernetes_cluster.by_name", "status", "active"),
				),
			},
			{
				// another node size: a new cluster
				Config: clusterConfig("prod-2", "k8s-4vcpu-8gb", 3),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_kubernetes_cluster.k", plancheck.ResourceActionDestroyBeforeCreate)},
				},
				Check: resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "node_size", "k8s-4vcpu-8gb"),
			},
		},
	})
	if len(f.clusters) != 0 {
		t.Fatalf("clusters left after destroy: %v", f.clusters)
	}
}

func TestKubernetesClusterGoneOutsideTerraform(t *testing.T) {
	f := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: clusterConfig("prod", "k8s-2vcpu-4gb", 2)},
			{
				PreConfig: func() {
					for id := range f.clusters {
						delete(f.clusters, id)
					}
				},
				Config: clusterConfig("prod", "k8s-2vcpu-4gb", 2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_kubernetes_cluster.k", plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}

func TestKubernetesClusterErrors(t *testing.T) {
	f := newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      clusterConfig("prod", "nope", 2),
				ExpectError: regexp.MustCompile(`Unknown node size nope`),
			},
			{
				// ends in error: the apply fails with the API's reason and the cluster stays in state (tainted)
				PreConfig:   func() { f.clusterFails = true },
				Config:      clusterConfig("prod", "k8s-2vcpu-4gb", 2),
				ExpectError: regexp.MustCompile(`no capacity in dhaka-1`),
			},
			{
				PreConfig: func() { f.clusterFails = false },
				Config:    clusterConfig("prod", "k8s-2vcpu-4gb", 2),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("cloudxino_kubernetes_cluster.k", plancheck.ResourceActionDestroyBeforeCreate)},
				},
			},
		},
	})
}

func TestKubernetesClusterDataSourceNeedsOneMatch(t *testing.T) {
	newFakeAPI(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{
				Config:      `data "cloudxino_kubernetes_cluster" "x" { name = "missing" }`,
				ExpectError: regexp.MustCompile(`0 clusters named "missing"`),
			},
		},
	})
}
