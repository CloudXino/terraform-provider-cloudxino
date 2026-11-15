# The team's default project
data "cloudxino_project" "default" {}

data "cloudxino_project" "staging" {
  name = "Staging"
}
