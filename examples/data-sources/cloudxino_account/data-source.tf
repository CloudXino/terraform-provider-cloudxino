data "cloudxino_account" "me" {}

output "team" {
  value = data.cloudxino_account.me.organization_name
}
