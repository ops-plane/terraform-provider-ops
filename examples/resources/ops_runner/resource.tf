# A runner for the production pool; its token goes into the Helm chart's secret.
resource "ops_runner" "prd" {
  name = "k8s-prd"
  pool = "prd"
}

output "runner_token" {
  value     = ops_runner.prd.token
  sensitive = true
}
