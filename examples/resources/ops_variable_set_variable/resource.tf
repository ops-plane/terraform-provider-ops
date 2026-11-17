resource "ops_variable_set_variable" "client_secret" {
  variable_set_id = ops_variable_set.azure_prd.id
  category        = "env"
  key             = "ARM_CLIENT_SECRET"
  value           = var.arm_client_secret
  sensitive       = true
}
