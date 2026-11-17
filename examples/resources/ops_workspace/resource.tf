# An empty workspace; its templates are added in OPS's UI.
resource "ops_workspace" "shop" {
  name            = "shop-prd"
  project_id      = ops_project.prd.id
  drift_detection = true
}
