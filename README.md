# terraform-provider-ops

Manage [OPS](https://github.com/ops-plane/ops)'s platform objects with Terraform: projects and their
environment values, workspaces and their variables, variable sets (and where they apply), VCS connections, runners,
registry modules published from a repository and the template catalog.
Templates on workspaces, runs and team access stay in OPS's UI.

On the Terraform Registry: [ops-plane/ops](https://registry.terraform.io/providers/ops-plane/ops/latest), with
its [documentation](https://registry.terraform.io/providers/ops-plane/ops/latest/docs).

```hcl
terraform {
  required_providers {
    ops = {
      source  = "ops-plane/ops"
      version = "~> 0.1"
    }
  }
}

provider "ops" {
  url   = "https://ops.example.com"
  token = var.ops_token # a service token; or OPSPLANE_URL and OPSPLANE_TOKEN
}

resource "ops_project" "prd" {
  name        = "prd"
  runner_pool = "prd"
}

resource "ops_variable_set" "azure_prd" { name = "azure-prd" }

resource "ops_variable_set_variable" "secret" {
  variable_set_id = ops_variable_set.azure_prd.id
  category        = "env"
  key             = "ARM_CLIENT_SECRET"
  value           = var.arm_client_secret
  sensitive       = true
}

resource "ops_variable_set_project" "azure_prd" {
  variable_set_id = ops_variable_set.azure_prd.id
  project_id      = ops_project.prd.id
}

resource "ops_workspace" "shop" {
  name       = "shop-prd"
  project_id = ops_project.prd.id
}
```

**The token** is a service token: Settings → API tokens in OPS, or `ops api-token terraform` on the server
(it has admin rights and is audited as `token:<name>`). Needs **OPS 0.10.0** or later; `ops_runner`, `ops_registry_module` and
`ops_template_catalog` need **0.11.0**.

Resources: `ops_project`, `ops_environment_value`, `ops_workspace`, `ops_workspace_variable`,
`ops_variable_set`, `ops_variable_set_variable`, `ops_variable_set_project`,
`ops_variable_set_workspace`, `ops_vcs_connection`, `ops_runner`, `ops_registry_module`,
`ops_template_catalog`. Data sources: `ops_project`, `ops_workspace`, `ops_variable_set`,
`ops_vcs_connection`. All importable (by name where they have one). Reference:
[Registry docs](https://registry.terraform.io/providers/ops-plane/ops/latest/docs).

Good to know:

- **Sensitive values are write-only** in OPS (variables, VCS tokens): Terraform keeps the configured value and
  can't see a change made to it in the UI. They're in Terraform's state: keep that encrypted.
- **Changes in the UI to managed objects** are reverted by the next apply, as with any provider.

## Development

```sh
go build ./... && go vet ./...
go generate ./...   # docs/ from the schema and examples/
```

Acceptance tests run real Terraform against a running OPS, creating and deleting objects with random names:

```sh
TF_ACC=1 OPSPLANE_URL=http://localhost:8080 OPSPLANE_TOKEN=<service token> go test -v ./...
# optional: a template's per-environment input, and a GitHub token OPS can check
OPSPLANE_ACC_TEMPLATE_ID=<id> OPSPLANE_ACC_INPUT=location OPSPLANE_ACC_VALUES=westeurope,northeurope
OPSPLANE_ACC_GITHUB_TOKEN=<token>   # also publishes a module of ops-plane/terraform-modules-examples
```

`TestAccTemplateCatalog` disconnects the catalog when it's done, so it skips an OPS whose catalog is connected:
run it against a fresh one.

CI (`.github/workflows/test.yml`) builds OPS's `master`, starts it on Postgres and runs them. A tag `v*` releases
(`.github/workflows/release.yml`, goreleaser), signed with the release key (`EC1FE5F6A3AFC49E`, the secrets
`GPG_PRIVATE_KEY` and `GPG_PASSPHRASE`); the Terraform Registry picks up each new tag.

## License

[Mozilla Public License 2.0](LICENSE).
