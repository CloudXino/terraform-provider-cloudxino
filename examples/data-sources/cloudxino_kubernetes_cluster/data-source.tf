# A cluster made in the console, by name; its kubeconfig is null unless it is active.
data "cloudxino_kubernetes_cluster" "prod" {
  name = "prod"
}
