# A CKS (managed Kubernetes) cluster and its kubeconfig.
#
#   export CLOUDXINO_TOKEN=cxk_...
#   terraform init && terraform apply              # about 5 minutes
#   terraform output -raw kubeconfig > kubeconfig.yaml
#   KUBECONFIG=kubeconfig.yaml kubectl get nodes
#
# The kubeconfig holds the cluster's admin credentials and is kept in the Terraform state.

terraform {
  required_providers {
    cloudxino = {
      source  = "cloudxino/cloudxino"
      version = "~> 0.1"
    }
  }
}

provider "cloudxino" {}

variable "node_count" {
  type    = number
  default = 2
}

resource "cloudxino_kubernetes_cluster" "k" {
  name       = "demo"
  node_size  = "k8s-2vcpu-4gb" # the console's create page lists the sizes
  node_count = var.node_count  # change it to resize the default node pool in place
  # region  = "dhaka-1"        # left out = the default region
  # version = "1.36.5"         # left out = the newest offered
}

output "endpoint" {
  value = cloudxino_kubernetes_cluster.k.endpoint
}

output "kubeconfig" {
  value     = cloudxino_kubernetes_cluster.k.kubeconfig
  sensitive = true
}
