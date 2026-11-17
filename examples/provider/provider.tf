terraform {
  required_providers {
    ops = {
      source = "ops-plane/ops"
    }
  }
}

# A service token: Settings → API tokens in OPS, or `ops api-token terraform` on the server.
# Or leave both out and set OPSPLANE_URL and OPSPLANE_TOKEN.
provider "ops" {
  url   = "https://ops.example.com"
  token = var.ops_token
}

variable "ops_token" {
  type      = string
  sensitive = true
}
