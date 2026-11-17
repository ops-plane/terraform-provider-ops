# A project per Azure subscription: its runs go to the runners of its pool.
resource "ops_project" "prd" {
  name        = "prd"
  description = "Production subscription"
  runner_pool = "prd"
}
