# The templates live in an Azure DevOps repository; the builder commits to it.
resource "ops_template_catalog" "this" {
  vcs_connection_id = ops_vcs_connection.ado.id
  repo              = "Infra/templates"
  branch            = "main"
  mode              = "commit"
}
