# A repository-linked workspace gets only sets attached to it.
resource "ops_variable_set_workspace" "azure_prd" {
  variable_set_id = ops_variable_set.azure_prd.id
  workspace_id    = ops_workspace.shop.id
}
