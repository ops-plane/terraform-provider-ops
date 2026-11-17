resource "ops_variable_set_project" "azure_prd" {
  variable_set_id = ops_variable_set.azure_prd.id
  project_id      = ops_project.prd.id
}
