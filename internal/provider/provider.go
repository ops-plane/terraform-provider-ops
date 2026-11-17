// Package provider is the OPS Terraform provider: OPS's platform objects (projects and their environment
// values, workspaces and their variables, variable sets, VCS connections) through its JSON API.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// New returns the provider of version (set at release).
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &opsProvider{version: version} }
}

type opsProvider struct{ version string }

type providerModel struct {
	URL   types.String `tfsdk:"url"`
	Token types.String `tfsdk:"token"`
}

func (p *opsProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName, resp.Version = "ops", p.version
}

func (p *opsProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages OPS's platform objects: projects and their environment values, workspaces and their variables, variable sets, VCS connections, runners, registry modules published from a repository and the template catalog. Templates on workspaces, runs and team access stay in OPS's UI.",
		Attributes: map[string]schema.Attribute{
			"url": schema.StringAttribute{
				Description: "OPS's address, e.g. `https://ops.example.com`. Default: the `OPSPLANE_URL` environment variable.",
				Optional:    true,
			},
			"token": schema.StringAttribute{
				Description: "A service token (Settings → API tokens, or `ops api-token <name>` on the server): admin rights, audited as `token:<name>`. Default: the `OPSPLANE_TOKEN` environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
		},
	}
}

func (p *opsProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var m providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	url, token := m.URL.ValueString(), m.Token.ValueString()
	if url == "" {
		url = os.Getenv("OPSPLANE_URL")
	}
	if token == "" {
		token = os.Getenv("OPSPLANE_TOKEN")
	}
	if url == "" || token == "" {
		resp.Diagnostics.AddError("OPS isn't configured",
			"Set the provider's url and token, or the OPSPLANE_URL and OPSPLANE_TOKEN environment variables. Create a token under Settings → API tokens.")
		return
	}
	c, err := client.New(url, token)
	if err != nil {
		resp.Diagnostics.AddAttributeError(pathRoot("url"), "Invalid url", err.Error())
		return
	}
	resp.ResourceData, resp.DataSourceData = c, c
}

func (p *opsProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newProjectResource, newEnvironmentValueResource, newWorkspaceResource, newWorkspaceVariableResource,
		newVariableSetResource, newVariableSetVariableResource, newVariableSetProjectResource, newVariableSetWorkspaceResource,
		newVCSConnectionResource, newRunnerResource, newRegistryModuleResource, newTemplateCatalogResource,
	}
}

func (p *opsProvider) DataSources(context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{newProjectDataSource, newWorkspaceDataSource, newVariableSetDataSource, newVCSConnectionDataSource}
}
