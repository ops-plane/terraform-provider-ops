package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// ops_registry_module: a module of OPS's registry published from a repository's tags (admins'). Destroying
// it stops publishing; the versions published stay, as workspaces pin them.

type registryModuleModel struct {
	ID           types.String `tfsdk:"id"`
	ConnectionID types.String `tfsdk:"vcs_connection_id"`
	Repo         types.String `tfsdk:"repo"`
	Dir          types.String `tfsdk:"dir"`
	TagPrefix    types.String `tfsdk:"tag_prefix"`
	Namespace    types.String `tfsdk:"namespace"`
	Name         types.String `tfsdk:"name"`
	System       types.String `tfsdk:"system"`
	Versions     types.List   `tfsdk:"versions"`
}

func (m *registryModuleModel) set(ctx context.Context, v client.VCSModule, diags *diag.Diagnostics) {
	m.ID = types.StringValue(v.Namespace + "/" + v.Name + "/" + v.System)
	m.ConnectionID, m.Repo, m.Dir, m.TagPrefix = types.StringValue(v.ConnectionID), types.StringValue(v.Repo), types.StringValue(v.Dir), types.StringValue(v.TagPrefix)
	m.Namespace, m.Name, m.System = types.StringValue(v.Namespace), types.StringValue(v.Name), types.StringValue(v.System)
	l, d := types.ListValueFrom(ctx, types.StringType, v.Versions)
	diags.Append(d...)
	m.Versions = l
}

type registryModuleResource struct{ base }

func newRegistryModuleResource() resource.Resource { return &registryModuleResource{} }

func (r *registryModuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_module"
}

func (r *registryModuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	address := []planmodifier.String{stringplanmodifier.RequiresReplace(), stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "A module of OPS's private registry, published from a repository: each tag (with tag_prefix) becomes a version, now and as new tags come. Destroying it stops publishing; the versions published stay, as workspaces pin them. Changing the source publishes it again.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Computed: true, Description: "<namespace>/<name>/<system>.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"vcs_connection_id": schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "The VCS connection that reads the repository."},
			"repo":              schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "`owner/repo` on GitHub, `project/repo` on Azure DevOps."},
			"dir": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), PlanModifiers: replace,
				Description: "The module's folder in the repository; empty: its root."},
			"tag_prefix": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), PlanModifiers: replace,
				Description: "Only tags starting with it publish, the rest being the version (`storage-account-` for `storage-account-v1.0.0`); empty: every semver tag."},
			"namespace": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: address, Description: "Its registry address; empty: from the repository."},
			"name":      schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: address},
			"system":    schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: address, Description: "Its provider, e.g. `azurerm`."},
			"versions": schema.ListAttribute{ElementType: types.StringType, Computed: true, Description: "The versions published so far.",
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *registryModuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m registryModuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := map[string]any{"connection_id": m.ConnectionID.ValueString(), "repo": m.Repo.ValueString(), "dir": m.Dir.ValueString(),
		"tag_prefix": m.TagPrefix.ValueString(), "namespace": m.Namespace.ValueString(), "name": m.Name.ValueString(), "system": m.System.ValueString()}
	var v client.VCSModule
	if err := r.c.Do(ctx, "POST", "/vcs-modules", in, &v); err != nil {
		apiError(&resp.Diagnostics, "Publishing the module", err)
		return
	}
	m.set(ctx, v, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *registryModuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m registryModuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var v client.VCSModule
	if err := r.c.Do(ctx, "GET", "/vcs-modules/"+m.ID.ValueString(), nil, &v); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the module", err)
		return
	}
	m.set(ctx, v, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Update can't happen: every attribute replaces it.
func (r *registryModuleResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
}

func (r *registryModuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m registryModuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Do(ctx, "DELETE", "/vcs-modules/"+m.ID.ValueString(), nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Unlinking the module", err)
	}
}

// ImportState takes "<namespace>/<name>/<system>".
func (r *registryModuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := splitID(req.ID, 3, "<namespace>/<name>/<system>"); err != nil {
		resp.Diagnostics.AddError("Importing the module", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), strings.TrimSpace(req.ID))...)
}
