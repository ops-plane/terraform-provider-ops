package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// ops_vcs_connection: an access token for GitHub or Azure DevOps (admins'). The token is write-only.

type vcsConnectionModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Provider     types.String `tfsdk:"provider_type"`
	BaseURL      types.String `tfsdk:"base_url"`
	Token        types.String `tfsdk:"token"`
	Scope        types.String `tfsdk:"scope"`
	ProjectIDs   types.Set    `tfsdk:"project_ids"`
	WorkspaceIDs types.Set    `tfsdk:"workspace_ids"`
}

func (m *vcsConnectionModel) set(ctx context.Context, c client.VCSConnection, diags *diag.Diagnostics) {
	m.ID, m.Name, m.Provider, m.BaseURL = types.StringValue(c.ID), types.StringValue(c.Name), types.StringValue(c.Provider), types.StringValue(c.BaseURL)
	m.Scope = types.StringValue(c.Scope)
	m.ProjectIDs, m.WorkspaceIDs = setOf(ctx, c.ProjectIDs, diags), setOf(ctx, c.WorkspaceIDs, diags)
}

type vcsConnectionResource struct{ base }

func newVCSConnectionResource() resource.Resource { return &vcsConnectionResource{} }

func (r *vcsConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vcs_connection"
}

func (r *vcsConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A VCS connection: an access token for GitHub or Azure DevOps, for registry modules, the template catalog and workspaces linked to a branch. The token is write-only: Terraform can't see a token replaced in the UI.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, PlanModifiers: replace},
			"provider_type": schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "`github` or `azure-devops`.",
				Validators: []validator.String{stringvalidator.OneOf("github", "azure-devops")}},
			"base_url": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: append(replace, stringplanmodifier.UseStateForUnknown()),
				Description: "The API URL: for Azure DevOps the organization (`https://dev.azure.com/<org>`); GitHub's by default."},
			"token": schema.StringAttribute{Required: true, Sensitive: true,
				Description: "The access token, checked when set (the provider lists repositories with it). Read access for modules and workspaces; write access for a template catalog the builder saves to."},
			"scope": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Description: "Which workspaces may link its repositories: `\"\"` none, `all`, or `selected` (`project_ids`, `workspace_ids`).",
				Validators:  []validator.String{stringvalidator.OneOf("", "all", "selected")}},
			"project_ids": schema.SetAttribute{ElementType: types.StringType, Optional: true, Computed: true,
				Description:   "With scope `selected`: every workspace of these projects may link it.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()}},
			"workspace_ids": schema.SetAttribute{ElementType: types.StringType, Optional: true, Computed: true,
				Description:   "With scope `selected`: these workspaces may link it.",
				PlanModifiers: []planmodifier.Set{setplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *vcsConnectionResource) body(ctx context.Context, m vcsConnectionModel, diags *diag.Diagnostics) map[string]any {
	in := map[string]any{"token": m.Token.ValueString(), "scope": m.Scope.ValueString()}
	if v := stringsOf(ctx, m.ProjectIDs, diags); v != nil {
		in["project_ids"] = *v
	}
	if v := stringsOf(ctx, m.WorkspaceIDs, diags); v != nil {
		in["workspace_ids"] = *v
	}
	return in
}

func (r *vcsConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m vcsConnectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := r.body(ctx, m, &resp.Diagnostics)
	in["name"], in["provider"] = m.Name.ValueString(), m.Provider.ValueString()
	if v := strPtr(m.BaseURL); v != nil {
		in["base_url"] = *v
	}
	var c client.VCSConnection
	if err := r.c.Do(ctx, "POST", "/vcs-connections", in, &c); err != nil {
		apiError(&resp.Diagnostics, "Creating the VCS connection", err)
		return
	}
	m.set(ctx, c, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *vcsConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m vcsConnectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var c client.VCSConnection
	if err := r.c.Do(ctx, "GET", client.Path("vcs-connections", m.ID.ValueString()), nil, &c); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the VCS connection", err)
		return
	}
	m.set(ctx, c, &resp.Diagnostics) // the token stays as configured: OPS doesn't return it
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *vcsConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, st vcsConnectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := r.body(ctx, m, &resp.Diagnostics)
	if m.Token.Equal(st.Token) { // unchanged: not checked against the provider again
		delete(in, "token")
	}
	var c client.VCSConnection
	if err := r.c.Do(ctx, "PATCH", client.Path("vcs-connections", st.ID.ValueString()), in, &c); err != nil {
		apiError(&resp.Diagnostics, "Changing the VCS connection", err)
		return
	}
	m.set(ctx, c, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *vcsConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m vcsConnectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Do(ctx, "DELETE", client.Path("vcs-connections", m.ID.ValueString()), nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Deleting the VCS connection", err)
	}
}

// ImportState takes the connection's id or name. The token isn't returned: set it in the configuration.
func (r *vcsConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := r.importID(ctx, "/vcs-connections", req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Importing the VCS connection", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// data "ops_vcs_connection": a VCS connection by name (never its token).

type vcsConnectionDataSource struct{ base }

func newVCSConnectionDataSource() datasource.DataSource { return &vcsConnectionDataSource{} }

func (d *vcsConnectionDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_vcs_connection"
}

func (d *vcsConnectionDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(req.ProviderData, &resp.Diagnostics)
}

func (d *vcsConnectionDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "A VCS connection, by name: e.g. to publish registry modules or connect the template catalog with a connection made in the UI.",
		Attributes: map[string]dschema.Attribute{
			"name":          dschema.StringAttribute{Required: true},
			"id":            dschema.StringAttribute{Computed: true},
			"provider_type": dschema.StringAttribute{Computed: true},
			"base_url":      dschema.StringAttribute{Computed: true},
			"scope":         dschema.StringAttribute{Computed: true},
		},
	}
}

type vcsConnectionDataModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Provider types.String `tfsdk:"provider_type"`
	BaseURL  types.String `tfsdk:"base_url"`
	Scope    types.String `tfsdk:"scope"`
}

func (d *vcsConnectionDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m vcsConnectionDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := d.byName(ctx, "/vcs-connections", m.Name.ValueString())
	var c client.VCSConnection
	if err == nil {
		err = d.c.Do(ctx, "GET", client.Path("vcs-connections", id), nil, &c)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Reading the VCS connection", err)
		return
	}
	m.ID, m.Provider, m.BaseURL, m.Scope = types.StringValue(c.ID), types.StringValue(c.Provider), types.StringValue(c.BaseURL), types.StringValue(c.Scope)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
