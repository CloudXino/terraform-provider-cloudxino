package provider

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudxino/cloudxino-go"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// TestAccStack runs a small stack against the real CloudXino API: apply, an empty plan,
// import of every resource, then destroy, and checks that the API no longer has any of it.
//
// It costs money, so it only runs with TF_ACC=1 and CLOUDXINO_TOKEN set (see the
// "acceptance" workflow). Use a test team's developer key, never a real team's.
//
//	CLOUDXINO_ACC_IMAGE  image slug (default debian-13)
//	CLOUDXINO_ACC_SIZE   size slug (default s-1vcpu-1gb, the smallest)
//	CLOUDXINO_ACC_DOMAIN zone to create (default tfacc-<random>.com; it is never delegated)
func TestAccStack(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests against the real API")
	}
	if os.Getenv("CLOUDXINO_TOKEN") == "" {
		t.Fatal("CLOUDXINO_TOKEN must be set for acceptance tests")
	}

	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	name := "tfacc-" + strings.ToLower(suffix)
	image := envOr("CLOUDXINO_ACC_IMAGE", "debian-13")
	size := envOr("CLOUDXINO_ACC_SIZE", "s-1vcpu-1gb")
	domain := envOr("CLOUDXINO_ACC_DOMAIN", name+".com")
	pub, err := randEd25519AuthorizedKey()
	if err != nil {
		t.Fatal(err)
	}

	config := fmt.Sprintf(`
resource "cloudxino_ssh_key" "k" {
  name       = %[1]q
  public_key = %[2]q
}

resource "cloudxino_server" "web" {
  name        = %[1]q
  image       = %[3]q
  size        = %[4]q
  ssh_key_ids = [cloudxino_ssh_key.k.id]
  tags        = [%[1]q]
}

resource "cloudxino_firewall" "web" {
  name = %[1]q
  inbound_rule = [
    { protocol = "tcp", ports = "80", sources = ["0.0.0.0/0"] },
    { protocol = "tcp", ports = "22", sources = ["0.0.0.0/0"] },
  ]
  tags = [%[1]q]
}

resource "cloudxino_load_balancer" "web" {
  name = %[1]q
  forwarding_rule = [
    { entry_protocol = "http", entry_port = 80, target_port = 80 },
  ]
  target_ids = [cloudxino_server.web.id]
}

resource "cloudxino_dns_zone" "z" {
  name = %[5]q
}

resource "cloudxino_dns_record" "www" {
  zone_id = cloudxino_dns_zone.z.id
  name    = "www"
  type    = "A"
  content = cloudxino_load_balancer.web.ip_address
  ttl     = 300
}
`, name, pub, image, size, domain)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             checkAccDestroyed,
		Steps: []resource.TestStep{
			{
				Config:           config,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("cloudxino_ssh_key.k", "fingerprint"),
					resource.TestCheckResourceAttr("cloudxino_server.web", "status", "running"),
					resource.TestCheckResourceAttrSet("cloudxino_server.web", "ip_address"),
					resource.TestCheckResourceAttrSet("cloudxino_load_balancer.web", "ip_address"),
					resource.TestCheckResourceAttrSet("cloudxino_dns_zone.z", "nameservers.#"),
					resource.TestCheckResourceAttr("cloudxino_dns_record.www", "fqdn", "www."+domain),
				),
			},
			{ResourceName: "cloudxino_ssh_key.k", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"public_key"}},
			{ResourceName: "cloudxino_server.web", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
			// server_count is live: the firewall was made before the tagged server existed, so state says 0 until a refresh.
			{ResourceName: "cloudxino_firewall.web", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"inbound_rule", "outbound_rule", "server_count"}},
			{ResourceName: "cloudxino_load_balancer.web", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts"}},
			{ResourceName: "cloudxino_dns_zone.z", ImportState: true, ImportStateVerify: true},
			{
				ResourceName: "cloudxino_dns_record.www", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					rs := s.RootModule().Resources["cloudxino_dns_record.www"]
					return rs.Primary.Attributes["zone_id"] + "/" + rs.Primary.ID, nil
				},
			},
		},
	})
}

// TestAccReservedIP reserves an address, points it at a server through an assignment, reads it
// back by address, imports both, then destroys everything. It skips when the API answers 503
// "unavailable" (no free address, or reserved IPs are turned off for the team's region).
//
//	CLOUDXINO_ACC_IMAGE, CLOUDXINO_ACC_SIZE as for TestAccStack
func TestAccReservedIP(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 to run acceptance tests against the real API")
	}
	if os.Getenv("CLOUDXINO_TOKEN") == "" {
		t.Fatal("CLOUDXINO_TOKEN must be set for acceptance tests")
	}
	skipUnlessReservedIPs(t)

	name := "tfacc-" + strings.ToLower(acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum))
	config := fmt.Sprintf(`
resource "cloudxino_server" "web" {
  name  = %[1]q
  image = %[2]q
  size  = %[3]q
}

resource "cloudxino_reserved_ip" "ip" {
  region = cloudxino_server.web.region
}

resource "cloudxino_reserved_ip_assignment" "web" {
  reserved_ip_id = cloudxino_reserved_ip.ip.id
  server_id      = cloudxino_server.web.id
}

data "cloudxino_reserved_ip" "by_address" {
  ip_address = cloudxino_reserved_ip_assignment.web.ip_address
}
`, name, envOr("CLOUDXINO_ACC_IMAGE", "debian-13"), envOr("CLOUDXINO_ACC_SIZE", "s-1vcpu-1gb"))

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             checkAccDestroyed,
		Steps: []resource.TestStep{
			{
				Config:           config,
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("cloudxino_reserved_ip.ip", "ip_address"),
					resource.TestCheckResourceAttrPair("cloudxino_reserved_ip_assignment.web", "server_id", "cloudxino_server.web", "id"),
					resource.TestCheckResourceAttrPair("data.cloudxino_reserved_ip.by_address", "id", "cloudxino_reserved_ip.ip", "id"),
					resource.TestCheckResourceAttrPair("data.cloudxino_reserved_ip.by_address", "server_id", "cloudxino_server.web", "id"),
					resource.TestCheckResourceAttr("data.cloudxino_reserved_ip.by_address", "status", "assigned"),
				),
			},
			{ResourceName: "cloudxino_reserved_ip.ip", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"status"}},
			{ResourceName: "cloudxino_reserved_ip_assignment.web", ImportState: true, ImportStateVerify: true},
		},
	})
}

// TestAccKubernetesCluster builds a one-node CKS cluster, checks its kubeconfig, resizes it to two
// nodes, imports it, reads it through the data source, then destroys it. A cluster takes about 5
// minutes and is billed per hour, so on top of TF_ACC it needs CLOUDXINO_ACC_CKS=1. It skips when
// the API answers 501 (CKS switched off).
//
//	CLOUDXINO_ACC_NODE_SIZE worker size slug (default k8s-2vcpu-4gb)
func TestAccKubernetesCluster(t *testing.T) {
	if os.Getenv("TF_ACC") == "" || os.Getenv("CLOUDXINO_ACC_CKS") == "" {
		t.Skip("set TF_ACC=1 and CLOUDXINO_ACC_CKS=1 to build a real (billed) CKS cluster")
	}
	if os.Getenv("CLOUDXINO_TOKEN") == "" {
		t.Fatal("CLOUDXINO_TOKEN must be set for acceptance tests")
	}
	skipUnlessCKS(t)

	name := "tfacc-" + strings.ToLower(acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum))
	size := envOr("CLOUDXINO_ACC_NODE_SIZE", "k8s-2vcpu-4gb")
	config := func(count int) string {
		return fmt.Sprintf(`
resource "cloudxino_kubernetes_cluster" "k" {
  name       = %[1]q
  node_size  = %[2]q
  node_count = %[3]d
}

data "cloudxino_kubernetes_cluster" "by_name" {
  name = cloudxino_kubernetes_cluster.k.name
}
`, name, size, count)
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: factories,
		CheckDestroy:             checkAccDestroyed,
		Steps: []resource.TestStep{
			{
				Config:           config(1),
				ConfigPlanChecks: noChanges,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "status", "active"),
					resource.TestCheckResourceAttrSet("cloudxino_kubernetes_cluster.k", "endpoint"),
					resource.TestCheckResourceAttrSet("cloudxino_kubernetes_cluster.k", "version"),
					resource.TestMatchResourceAttr("cloudxino_kubernetes_cluster.k", "kubeconfig", regexp.MustCompile(`kind: Config`)),
					resource.TestCheckResourceAttrPair("data.cloudxino_kubernetes_cluster.by_name", "id", "cloudxino_kubernetes_cluster.k", "id"),
				),
			},
			{
				Config:           config(2),
				ConfigPlanChecks: noChanges,
				Check:            resource.TestCheckResourceAttr("cloudxino_kubernetes_cluster.k", "node_count", "2"),
			},
			{ResourceName: "cloudxino_kubernetes_cluster.k", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"timeouts", "kubeconfig", "status"}},
		},
	})
}

// skipUnlessCKS skips when the API has CKS switched off (creating a cluster answers 501).
func skipUnlessCKS(t *testing.T) {
	t.Helper()
	c, err := accClient()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	// the API checks the CKS switch before anything else and refuses an unknown size (400)
	// before it creates anything: 501 = off, anything else = on
	_, err = c.Clusters.Create(ctx, cloudxino.CreateClusterRequest{Name: "tfacc-probe", NodeSize: "tfacc-no-such-size"})
	var apiErr *cloudxino.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotImplemented {
		t.Skipf("CKS is switched off on this API: %v", err)
	}
	if err == nil {
		t.Fatal("the probe created a cluster named tfacc-probe; delete it in the console")
	}
}

// skipUnlessReservedIPs reserves an address and gives it straight back; a 503 "unavailable" skips the test.
func skipUnlessReservedIPs(t *testing.T) {
	t.Helper()
	c, err := accClient()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ip, err := c.ReservedIPs.Create(ctx, cloudxino.CreateReservedIPRequest{})
	if cloudxino.IsUnavailable(err) {
		t.Skipf("reserved IPs are not available on this API: %v", err)
	}
	if err != nil {
		t.Fatalf("probing reserved IPs: %v", err)
	}
	if err := c.ReservedIPs.Delete(ctx, ip.Id); err != nil {
		t.Fatalf("giving back the probe reserved IP %s (%s): %v", ip.Id, *ip.IpAddress, err)
	}
}

func accClient() (*cloudxino.Client, error) {
	opts := []cloudxino.Option{}
	if u := os.Getenv("CLOUDXINO_API_URL"); u != "" {
		opts = append(opts, cloudxino.WithBaseURL(u))
	}
	return cloudxino.NewClient(os.Getenv("CLOUDXINO_TOKEN"), opts...)
}

// randEd25519AuthorizedKey makes a throwaway ed25519 public key in authorized_keys form.
// (acctest.RandSSHKeyPair makes 1024-bit RSA, which the API rejects.)
func randEd25519AuthorizedKey() (string, error) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	var b []byte
	for _, part := range [][]byte{[]byte("ssh-ed25519"), pub} {
		b = binary.BigEndian.AppendUint32(b, uint32(len(part)))
		b = append(b, part...)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(b) + " tfacc@cloudxino", nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// checkAccDestroyed asks the API for every resource the test made and fails if any is still there.
// A server may linger briefly in a deleting state, which counts as gone.
func checkAccDestroyed(s *terraform.State) error {
	c, err := accClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var left []string
	for _, rs := range s.RootModule().Resources {
		id := rs.Primary.ID
		var err error
		switch rs.Type {
		case "cloudxino_ssh_key":
			_, err = c.SSHKeys.Get(ctx, id)
		case "cloudxino_server":
			var srv *cloudxino.Server
			srv, err = c.Servers.Get(ctx, id)
			if err == nil && srv.Status != nil && strings.HasPrefix(strings.ToLower(*srv.Status), "delet") {
				continue
			}
		case "cloudxino_firewall":
			_, err = c.Firewalls.Get(ctx, id)
		case "cloudxino_load_balancer":
			_, err = c.LoadBalancers.Get(ctx, id)
		case "cloudxino_dns_zone":
			_, err = c.DNS.GetZone(ctx, id)
		case "cloudxino_dns_record":
			_, err = c.DNS.GetRecord(ctx, rs.Primary.Attributes["zone_id"], id)
		case "cloudxino_reserved_ip":
			var ip *cloudxino.ReservedIP
			ip, err = c.ReservedIPs.Get(ctx, id)
			if err == nil && ip.Status != nil && *ip.Status == cloudxino.ReservedIPStatusDeleting {
				continue
			}
		case "cloudxino_kubernetes_cluster":
			var cl *cloudxino.Cluster
			cl, err = c.Clusters.Get(ctx, id)
			if err == nil && cl.Status == cloudxino.ClusterDeleting {
				continue
			}
		default:
			continue
		}
		switch {
		case err == nil:
			left = append(left, rs.Type+" "+id)
		case !cloudxino.IsNotFound(err):
			return fmt.Errorf("checking %s %s: %w", rs.Type, id, err)
		}
	}
	if len(left) > 0 {
		return fmt.Errorf("still present after destroy: %s", strings.Join(left, ", "))
	}
	return nil
}
