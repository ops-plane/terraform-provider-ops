package provider

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// Acceptance tests run real Terraform against a real OPS (TF_ACC=1, OPSPLANE_URL, OPSPLANE_TOKEN: a service
// token). Every object they create has a random suffix and is destroyed at the end.

var factories = map[string]func() (tfprotov6.ProviderServer, error){
	"ops": providerserver.NewProtocol6WithError(New("test")()),
}

func preCheck(t *testing.T) {
	t.Helper()
	for _, k := range []string{"OPSPLANE_URL", "OPSPLANE_TOKEN"} {
		if os.Getenv(k) == "" {
			t.Fatalf("%s is not set: acceptance tests need a running OPS and a service token", k)
		}
	}
}

func suffix() string { return fmt.Sprintf("%x", time.Now().UnixNano())[8:] }

// TestAccProject: a project is created, changed in place, imported by name, and read by a data source.
func TestAccProject(t *testing.T) {
	name := "acc-" + suffix()
	cfg := func(desc string) string {
		return fmt.Sprintf(`
resource "ops_project" "p" {
  name        = %q
  description = %q
  runner_pool = "acc"
}
data "ops_project" "p" {
  name       = ops_project.p.name
  depends_on = [ops_project.p]
}`, name, desc)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: cfg("first"), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("ops_project.p", "description", "first"),
				resource.TestCheckResourceAttr("ops_project.p", "runner_pool", "acc"),
				resource.TestCheckResourceAttr("ops_project.p", "default", "false"),
				resource.TestCheckResourceAttrPair("data.ops_project.p", "id", "ops_project.p", "id"))},
			{Config: cfg("second"), Check: resource.TestCheckResourceAttr("ops_project.p", "description", "second")},
			{ResourceName: "ops_project.p", ImportState: true, ImportStateId: name, ImportStateVerify: true},
		},
	})
}

// TestAccWorkspace: a project, an empty workspace in it with its settings and variables (a sensitive one write-only),
// the workspace renamed in place (same id), imported back.
func TestAccWorkspace(t *testing.T) {
	sfx := suffix()
	cfg := func(ws, plain string) string {
		return fmt.Sprintf(`
resource "ops_project" "p" { name = "acc-%[1]s" }
resource "ops_workspace" "w" {
  name            = %[2]q
  project_id      = ops_project.p.id
  drift_detection = true
}
resource "ops_workspace_variable" "plain" {
  workspace_id = ops_workspace.w.id
  category     = "terraform"
  key          = "region"
  value        = %[3]q
}
resource "ops_workspace_variable" "secret" {
  workspace_id = ops_workspace.w.id
  category     = "env"
  key          = "ARM_CLIENT_SECRET"
  value        = "s3cret"
  sensitive    = true
}
resource "ops_workspace_variable" "tags" {
  workspace_id = ops_workspace.w.id
  category     = "terraform"
  key          = "tags"
  value        = "{env = \"acc\"}"
  hcl          = true
}`, sfx, ws, plain)
	}
	var firstID string
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: cfg("acc-ws-"+sfx, "westeurope"), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttrPair("ops_workspace.w", "project_id", "ops_project.p", "id"),
				resource.TestCheckResourceAttr("ops_workspace.w", "execution_mode", "remote"),
				resource.TestCheckResourceAttr("ops_workspace.w", "drift_detection", "true"),
				resource.TestCheckResourceAttr("ops_workspace_variable.secret", "value", "s3cret"), // kept: not returned
				func(s *terraform.State) error {
					firstID = s.RootModule().Resources["ops_workspace.w"].Primary.ID
					return nil
				})},
			{Config: cfg("acc-ws2-"+sfx, "northeurope"), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("ops_workspace.w", "name", "acc-ws2-"+sfx),
				resource.TestCheckResourceAttr("ops_workspace_variable.plain", "value", "northeurope"),
				func(s *terraform.State) error {
					if id := s.RootModule().Resources["ops_workspace.w"].Primary.ID; id != firstID {
						return fmt.Errorf("renaming replaced the workspace: %s → %s", firstID, id)
					}
					return nil
				})},
			{ResourceName: "ops_workspace.w", ImportState: true, ImportStateId: "acc-ws2-" + sfx, ImportStateVerify: true},
			{ResourceName: "ops_workspace_variable.plain", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: func(s *terraform.State) (string, error) {
					r := s.RootModule().Resources["ops_workspace_variable.plain"].Primary
					return r.Attributes["workspace_id"] + "/" + r.ID, nil
				}},
		},
	})
}

// TestAccVariableSet: a set with a variable, attached to a project and a workspace, imported back.
func TestAccVariableSet(t *testing.T) {
	sfx := suffix()
	cfg := fmt.Sprintf(`
resource "ops_project" "p" { name = "acc-%[1]s" }
resource "ops_workspace" "w" {
  name       = "acc-ws-%[1]s"
  project_id = ops_project.p.id
}
resource "ops_variable_set" "s" {
  name     = "acc-set-%[1]s"
  priority = true
}
resource "ops_variable_set_variable" "v" {
  variable_set_id = ops_variable_set.s.id
  category        = "env"
  key             = "ARM_SUBSCRIPTION_ID"
  value           = "00000000-0000-0000-0000-000000000001"
}
resource "ops_variable_set_project" "p" {
  variable_set_id = ops_variable_set.s.id
  project_id      = ops_project.p.id
}
resource "ops_variable_set_workspace" "w" {
  variable_set_id = ops_variable_set.s.id
  workspace_id    = ops_workspace.w.id
}
data "ops_variable_set" "s" {
  name       = ops_variable_set.s.name
  depends_on = [ops_variable_set.s]
}`, sfx)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("ops_variable_set.s", "priority", "true"),
				resource.TestCheckResourceAttrPair("data.ops_variable_set.s", "id", "ops_variable_set.s", "id"))},
			{ResourceName: "ops_variable_set.s", ImportState: true, ImportStateId: "acc-set-" + sfx, ImportStateVerify: true},
			{ResourceName: "ops_variable_set_project.p", ImportState: true, ImportStateVerify: true},
			{ResourceName: "ops_variable_set_workspace.w", ImportState: true, ImportStateVerify: true},
		},
	})
}

// TestAccEnvironmentValue sets a project's value for a template's per-environment input: needs such a template in the
// catalog (OPSPLANE_ACC_TEMPLATE_ID, OPSPLANE_ACC_INPUT, OPSPLANE_ACC_VALUES: two allowed values, comma-separated).
func TestAccEnvironmentValue(t *testing.T) {
	tpl, input, values := os.Getenv("OPSPLANE_ACC_TEMPLATE_ID"), os.Getenv("OPSPLANE_ACC_INPUT"), strings.Split(os.Getenv("OPSPLANE_ACC_VALUES"), ",")
	if tpl == "" || input == "" || len(values) != 2 {
		t.Skip("OPSPLANE_ACC_TEMPLATE_ID, OPSPLANE_ACC_INPUT and OPSPLANE_ACC_VALUES name a template's per-environment input")
	}
	sfx := suffix()
	cfg := func(value string) string {
		return fmt.Sprintf(`
resource "ops_project" "p" { name = "acc-%s" }
resource "ops_environment_value" "v" {
  project_id  = ops_project.p.id
  template_id = %q
  input       = %q
  value       = %q
}`, sfx, tpl, input, value)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: cfg(values[0]), Check: resource.TestCheckResourceAttr("ops_environment_value.v", "value", values[0])},
			{Config: cfg(values[1]), Check: resource.TestCheckResourceAttr("ops_environment_value.v", "value", values[1])},
			{ResourceName: "ops_environment_value.v", ImportState: true, ImportStateVerify: true},
		},
	})
}

// TestAccVCSConnection creates a GitHub connection: needs a token GitHub accepts (OPSPLANE_ACC_GITHUB_TOKEN), as
// OPS checks it.
func TestAccVCSConnection(t *testing.T) {
	token := os.Getenv("OPSPLANE_ACC_GITHUB_TOKEN")
	if token == "" {
		t.Skip("OPSPLANE_ACC_GITHUB_TOKEN: a GitHub token OPS can check")
	}
	sfx := suffix()
	cfg := func(scope string) string {
		return fmt.Sprintf(`
variable "token" {
  type      = string
  sensitive = true
}
resource "ops_vcs_connection" "c" {
  name          = "acc-%s"
  provider_type = "github"
  token         = var.token
  scope         = %q
}`, sfx, scope)
	}
	t.Setenv("TF_VAR_token", token)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: cfg(""), Check: resource.TestCheckResourceAttr("ops_vcs_connection.c", "provider_type", "github")},
			{Config: cfg("all"), Check: resource.TestCheckResourceAttr("ops_vcs_connection.c", "scope", "all")},
			{ResourceName: "ops_vcs_connection.c", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"token"}},
		},
	})
}

// TestAccRunner: a runner with a token OPS creates (kept in the state), and one with a token from a secret store.
func TestAccRunner(t *testing.T) {
	sfx := suffix()
	cfg := fmt.Sprintf(`
resource "ops_runner" "made" {
  name = "acc-%[1]s"
  pool = "acc"
}
resource "ops_runner" "own" {
  name  = "acc-own-%[1]s"
  token = %[2]q
}`, sfx, strings.Repeat("t", 40))
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: cfg, Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("ops_runner.made", "pool", "acc"),
				resource.TestCheckResourceAttrWith("ops_runner.made", "token", func(v string) error {
					if !strings.HasPrefix(v, "opsr_") {
						return fmt.Errorf("a created runner's token: %q", v)
					}
					return nil
				}),
				resource.TestCheckResourceAttr("ops_runner.own", "pool", "default"))},
			{ResourceName: "ops_runner.made", ImportState: true, ImportStateId: "acc-" + sfx, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"token"}},
		},
	})
}

// TestAccRegistryModule publishes a module from a public GitHub repository's tags: needs a GitHub token OPS can
// check (OPSPLANE_ACC_GITHUB_TOKEN); ops-plane/terraform-modules-examples' storage-account by default
// (OPSPLANE_ACC_MODULE_REPO, OPSPLANE_ACC_MODULE_DIR: its folder and tag prefix <dir>-).
func TestAccRegistryModule(t *testing.T) {
	token := os.Getenv("OPSPLANE_ACC_GITHUB_TOKEN")
	if token == "" {
		t.Skip("OPSPLANE_ACC_GITHUB_TOKEN: a GitHub token OPS can check")
	}
	repo, dir := envOr("OPSPLANE_ACC_MODULE_REPO", "ops-plane/terraform-modules-examples"), envOr("OPSPLANE_ACC_MODULE_DIR", "storage-account")
	sfx := suffix()
	t.Setenv("TF_VAR_token", token)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { preCheck(t) },
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: fmt.Sprintf(`
variable "token" {
  type      = string
  sensitive = true
}
resource "ops_vcs_connection" "gh" {
  name          = "acc-%[1]s"
  provider_type = "github"
  token         = var.token
}
data "ops_vcs_connection" "gh" {
  name       = ops_vcs_connection.gh.name
  depends_on = [ops_vcs_connection.gh]
}
resource "ops_registry_module" "m" {
  vcs_connection_id = data.ops_vcs_connection.gh.id
  repo              = %[2]q
  dir               = %[3]q
  tag_prefix        = "%[3]s-"
  namespace         = "acc%[1]s"
  name              = %[3]q
  system            = "azurerm"
}`, sfx, repo, dir), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("ops_registry_module.m", "id", "acc"+sfx+"/"+dir+"/azurerm"),
				resource.TestCheckResourceAttrWith("ops_registry_module.m", "versions.#", func(v string) error {
					if v == "0" {
						return fmt.Errorf("no version published from the tags")
					}
					return nil
				}))},
			{ResourceName: "ops_registry_module.m", ImportState: true, ImportStateVerify: true},
		},
	})
}

// TestAccTemplateCatalog connects the template catalog to a GitHub repository (OPSPLANE_ACC_GITHUB_TOKEN; the repo of
// TestAccRegistryModule, a folder without templates) and disconnects it. Destroying it disconnects the catalog, so it
// refuses to run on an OPS whose catalog is connected already.
func TestAccTemplateCatalog(t *testing.T) {
	token := os.Getenv("OPSPLANE_ACC_GITHUB_TOKEN")
	if token == "" {
		t.Skip("OPSPLANE_ACC_GITHUB_TOKEN: a GitHub token OPS can check")
	}
	preCheck(t)
	c := testClient(t)
	if err := c.Do(context.Background(), "GET", "/template-catalog", nil, nil); err == nil {
		t.Skip("this OPS's template catalog is connected: the test would disconnect it")
	}
	repo, dir := envOr("OPSPLANE_ACC_MODULE_REPO", "ops-plane/terraform-modules-examples"), envOr("OPSPLANE_ACC_MODULE_DIR", "storage-account")
	t.Setenv("TF_VAR_token", token)
	cfg := func(mode string) string {
		return fmt.Sprintf(`
variable "token" {
  type      = string
  sensitive = true
}
resource "ops_vcs_connection" "gh" {
  name          = "acc-cat-%[1]s"
  provider_type = "github"
  token         = var.token
}
resource "ops_template_catalog" "c" {
  vcs_connection_id = ops_vcs_connection.gh.id
  repo              = %[2]q
  dir               = %[3]q
  mode              = %[4]q
}`, suffixOnce, repo, dir, mode)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() {},
		ProtoV6ProviderFactories: factories,
		Steps: []resource.TestStep{
			{Config: cfg("off"), Check: resource.ComposeTestCheckFunc(
				resource.TestCheckResourceAttr("ops_template_catalog.c", "branch", "main"),
				resource.TestCheckResourceAttrSet("ops_template_catalog.c", "commit"))},
			{Config: cfg("pr"), Check: resource.TestCheckResourceAttr("ops_template_catalog.c", "mode", "pr")},
			{ResourceName: "ops_template_catalog.c", ImportState: true, ImportStateVerify: true},
		},
	})
}

var suffixOnce = suffix()

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

func testClient(t *testing.T) *client.Client {
	t.Helper()
	c, err := client.New(os.Getenv("OPSPLANE_URL"), os.Getenv("OPSPLANE_TOKEN"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}
