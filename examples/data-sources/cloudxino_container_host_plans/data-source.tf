# The plans a container host can use, cheapest first, with monthly prices in BDT.
data "cloudxino_container_host_plans" "all" {}

output "cheapest_plan" {
  value = data.cloudxino_container_host_plans.all.plans[0].slug
}
