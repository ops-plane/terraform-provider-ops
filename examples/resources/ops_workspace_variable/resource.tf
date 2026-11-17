resource "ops_workspace_variable" "tags" {
  workspace_id = ops_workspace.shop.id
  category     = "terraform"
  key          = "tags"
  value        = "{team = \"shop\"}"
  hcl          = true
}
