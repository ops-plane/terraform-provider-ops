# Every workspace of the project deploys this template in its subscription's region.
resource "ops_environment_value" "region" {
  project_id  = ops_project.prd.id
  template_id = "storage-account"
  input       = "location"
  value       = "swedencentral"
}
