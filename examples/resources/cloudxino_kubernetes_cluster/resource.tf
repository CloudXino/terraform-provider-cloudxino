# A CKS cluster with two worker nodes. Changing node_count resizes the default node pool in place.
resource "cloudxino_kubernetes_cluster" "prod" {
  name       = "prod"
  node_size  = "k8s-2vcpu-4gb"
  node_count = 2

  timeouts = {
    create = "40m"
  }
}

# Admin credentials for kubectl; kept in the Terraform state.
output "kubeconfig" {
  value     = cloudxino_kubernetes_cluster.prod.kubeconfig
  sensitive = true
}
