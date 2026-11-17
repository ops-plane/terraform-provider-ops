package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// ops_template_catalog: where the templates come from, one per installation (admins'). Destroying it disconnects
// the catalog: the templates come from TEMPLATES_DIR again.

type templateCatalogModel struct {
	ID           types.String `tfsdk:"id"`
	ConnectionID types.String `tfsdk:"vcs_connection_id"`
	Repo         types.String `tfsdk:"repo"`
	Branch       types.String `tfsdk:"branch"`
	Dir          types.String `tfsdk:"dir"`
	Mode         types.String `tfsdk:"mode"`
	Commit       types.String `tfsdk:"commit"`
	SyncError    types.String `tfsdk:"sync_error"`
}

func (m *templateCatalogModel) set(c client.TemplateCatalog) {
	m.ID = types.StringValue("template-catalog")
	m.ConnectionID, m.Repo, m.Branch, m.Dir, m.Mode = types.StringValue(c.ConnectionID), types.StringValue(c.Repo), types.StringValue(c.Branch),
		types.StringValue(c.Dir), types.StringValue(c.Mode)
	m.Commit, m.SyncError = types.StringValue(c.Commit), types.StringValue(c.SyncError)
}

type templateCatalogResource struct{ base }

func newTemplateCatalogResource() resource.Resource { return &templateCatalogResource{} }

func (r *templateCatalogResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_template_catalog"
}

func (r *templateCatalogResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computed := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "The template catalog: a folder of JSON templates in a repository branch, one per installation; OPS loads each new commit. Destroying it disconnects the catalog: the templates come from TEMPLATES_DIR again.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Computed: true, PlanModifiers: computed},
			"vcs_connection_id": schema.StringAttribute{Required: true, Description: "The VCS connection that reads it; with mode `commit` or `pr` one with write access."},
			"repo":              schema.StringAttribute{Required: true, Description: "`owner/repo` on GitHub, `project/repo` on Azure DevOps."},
			"branch":            schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("main")},
			"dir":               schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""), Description: "The folder of the templates; empty: the root."},
			"mode": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("off"),
				Description: "What saving a template in OPS's builder does: `off` (templates are edited in Git only), `commit` (to the branch) or `pr` (a pull request).",
				Validators:  []validator.String{stringvalidator.OneOf("off", "commit", "pr")}},
			"commit":     schema.StringAttribute{Computed: true, Description: "The commit loaded."},
			"sync_error": schema.StringAttribute{Computed: true, Description: "Why the last commit didn't load (an invalid template), if it didn't: the catalog keeps the templates it had."},
		},
	}
}

func (r *templateCatalogResource) put(ctx context.Context, m *templateCatalogModel) error {
	var c client.TemplateCatalog
	in := map[string]any{"connection_id": m.ConnectionID.ValueString(), "repo": m.Repo.ValueString(), "branch": m.Branch.ValueString(),
		"dir": m.Dir.ValueString(), "mode": m.Mode.ValueString()}
	if err := r.c.Do(ctx, "PUT", "/template-catalog", in, &c); err != nil {
		return err
	}
	m.set(c)
	return nil
}

func (r *templateCatalogResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m templateCatalogModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.put(ctx, &m); err != nil {
		apiError(&resp.Diagnostics, "Connecting the template catalog", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if s := m.SyncError.ValueString(); s != "" {
		resp.Diagnostics.AddWarning("The catalog is connected, but its templates didn't load", s)
	}
}

func (r *templateCatalogResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m templateCatalogModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var c client.TemplateCatalog
	if err := r.c.Do(ctx, "GET", "/template-catalog", nil, &c); err != nil {
		if client.IsNotFound(err) { // disconnected in the UI
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the template catalog", err)
		return
	}
	m.set(c)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *templateCatalogResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m templateCatalogModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.put(ctx, &m); err != nil {
		apiError(&resp.Diagnostics, "Changing the template catalog", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
	if s := m.SyncError.ValueString(); s != "" {
		resp.Diagnostics.AddWarning("The catalog is saved, but its templates didn't load", s)
	}
}

func (r *templateCatalogResource) Delete(ctx context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	if err := r.c.Do(ctx, "DELETE", "/template-catalog", nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Disconnecting the template catalog", err)
	}
}

// ImportState takes any id: there is one catalog.
func (r *templateCatalogResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("id"), "template-catalog")...)
}
