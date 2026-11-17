package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// ops_workspace: an empty workspace and its settings. Its templates are added in OPS's UI.

type workspaceModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	ProjectID      types.String `tfsdk:"project_id"`
	AutoApply      types.Bool   `tfsdk:"auto_apply"`
	ExecutionMode  types.String `tfsdk:"execution_mode"`
	DriftDetection types.Bool   `tfsdk:"drift_detection"`
	ForceDelete    types.Bool   `tfsdk:"force_delete"`
}

func (m *workspaceModel) set(w client.Workspace) {
	m.ID, m.Name, m.ProjectID = types.StringValue(w.ID), types.StringValue(w.Name), types.StringValue(w.ProjectID)
	m.AutoApply, m.ExecutionMode, m.DriftDetection = types.BoolValue(w.AutoApply), types.StringValue(w.ExecutionMode), types.BoolValue(w.DriftDetection)
}

type workspaceResource struct{ base }

func newWorkspaceResource() resource.Resource { return &workspaceResource{} }

func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *workspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A workspace: one Terraform state and its runs. Created empty; templates are added to it in OPS's UI (adding one starts a plan).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The workspace's id: it stays when the workspace is renamed or moved.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "2–63 lowercase letters, digits and dashes, unique across projects; its URL."},
			"project_id": schema.StringAttribute{Optional: true, Computed: true,
				Description:   "The project it's in; changing it moves the workspace (operate access on both). Default: the default project.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"auto_apply": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Apply plans without asking for confirmation."},
			"execution_mode": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("remote"),
				Description: "`remote`: runs on OPS's runners; `local`: the Terraform CLI runs (a `cloud` block), OPS keeps the state.",
				Validators:  []validator.String{stringvalidator.OneOf("remote", "local")}},
			"drift_detection": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "A daily drift check (a refresh-only plan, never applied)."},
			"force_delete": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Delete the workspace even while its state still manages resources (admins): they stay, unmanaged. Off: deleting such a workspace fails; destroy its resources first."},
		},
	}
}

func (r *workspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := map[string]any{"name": m.Name.ValueString(), "auto_apply": m.AutoApply.ValueBool(), "execution_mode": m.ExecutionMode.ValueString(),
		"drift_detection": m.DriftDetection.ValueBool()}
	if v := strPtr(m.ProjectID); v != nil {
		in["project_id"] = *v
	}
	var w client.Workspace
	if err := r.c.Do(ctx, "POST", "/workspaces", in, &w); err != nil {
		apiError(&resp.Diagnostics, "Creating the workspace", err)
		return
	}
	m.set(w)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *workspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var w client.Workspace
	if err := r.c.Do(ctx, "GET", client.Path("workspaces", m.ID.ValueString()), nil, &w); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the workspace", err)
		return
	}
	m.set(w)
	if m.ForceDelete.IsNull() { // imported
		m.ForceDelete = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *workspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, st workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := map[string]any{"name": m.Name.ValueString(), "auto_apply": m.AutoApply.ValueBool(), "execution_mode": m.ExecutionMode.ValueString(),
		"drift_detection": m.DriftDetection.ValueBool()}
	if v := strPtr(m.ProjectID); v != nil {
		in["project_id"] = *v
	}
	var w client.Workspace
	if err := r.c.Do(ctx, "PATCH", client.Path("workspaces", st.ID.ValueString()), in, &w); err != nil {
		apiError(&resp.Diagnostics, "Changing the workspace", err)
		return
	}
	m.set(w)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *workspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := client.Path("workspaces", m.ID.ValueString())
	if m.ForceDelete.ValueBool() {
		p += "?force=1"
	}
	if err := r.c.Do(ctx, "DELETE", p, nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Deleting the workspace", err)
	}
}

// ImportState takes the workspace's id or name.
func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := r.importID(ctx, "/workspaces", req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Importing the workspace", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// data "ops_workspace": a workspace by name.

type workspaceDataSource struct{ base }

func newWorkspaceDataSource() datasource.DataSource { return &workspaceDataSource{} }

func (d *workspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *workspaceDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(req.ProviderData, &resp.Diagnostics)
}

func (d *workspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "A workspace, by name.",
		Attributes: map[string]dschema.Attribute{
			"name":            dschema.StringAttribute{Required: true},
			"id":              dschema.StringAttribute{Computed: true},
			"project_id":      dschema.StringAttribute{Computed: true},
			"auto_apply":      dschema.BoolAttribute{Computed: true},
			"execution_mode":  dschema.StringAttribute{Computed: true},
			"drift_detection": dschema.BoolAttribute{Computed: true},
		},
	}
}

type workspaceDataModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	ProjectID      types.String `tfsdk:"project_id"`
	AutoApply      types.Bool   `tfsdk:"auto_apply"`
	ExecutionMode  types.String `tfsdk:"execution_mode"`
	DriftDetection types.Bool   `tfsdk:"drift_detection"`
}

func (d *workspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m workspaceDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := d.byName(ctx, "/workspaces", m.Name.ValueString())
	var w client.Workspace
	if err == nil {
		err = d.c.Do(ctx, "GET", client.Path("workspaces", id), nil, &w)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Reading the workspace", err)
		return
	}
	m.ID, m.ProjectID = types.StringValue(w.ID), types.StringValue(w.ProjectID)
	m.AutoApply, m.ExecutionMode, m.DriftDetection = types.BoolValue(w.AutoApply), types.StringValue(w.ExecutionMode), types.BoolValue(w.DriftDetection)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
