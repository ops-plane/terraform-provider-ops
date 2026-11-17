package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// ops_project: a project (a group of workspaces: its team access, variable sets, runner pool and environment
// values reach them all). Admins'.

type projectModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	RunnerPool  types.String `tfsdk:"runner_pool"`
	Default     types.Bool   `tfsdk:"default"`
}

func (m *projectModel) set(p client.Project) {
	m.ID, m.Name, m.Description = types.StringValue(p.ID), types.StringValue(p.Name), types.StringValue(p.Description)
	m.RunnerPool, m.Default = types.StringValue(p.RunnerPool), types.BoolValue(p.Default)
}

type projectResource struct{ base }

func newProjectResource() resource.Resource { return &projectResource{} }

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A project: a group of workspaces, such as an environment or a cloud subscription. Its team access, variable sets, runner pool and environment values reach all its workspaces.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "The project's id.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, Description: "2–63 lowercase letters, digits and dashes; its URL. Renaming keeps its workspaces."},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				Description: "Shown on its overview."},
			"runner_pool": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("default"),
				Description: "Only runners of this pool get its workspaces' runs, and their variables. Default: `default`."},
			"default": schema.BoolAttribute{Optional: true, Computed: true,
				Description:   "New workspaces land here unless another project is picked. Setting it makes this the default project; unsetting it isn't possible (make another the default).",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var p client.Project
	in := map[string]any{"name": m.Name.ValueString(), "description": m.Description.ValueString(), "runner_pool": m.RunnerPool.ValueString()}
	if v := boolPtr(m.Default); v != nil {
		in["default"] = *v
	}
	if err := r.c.Do(ctx, "POST", "/projects", in, &p); err != nil {
		apiError(&resp.Diagnostics, "Creating the project", err)
		return
	}
	m.set(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var p client.Project
	if err := r.c.Do(ctx, "GET", client.Path("projects", m.ID.ValueString()), nil, &p); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the project", err)
		return
	}
	m.set(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m, st projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &st)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := map[string]any{"name": m.Name.ValueString(), "description": m.Description.ValueString(), "runner_pool": m.RunnerPool.ValueString()}
	if v := boolPtr(m.Default); v != nil && *v != st.Default.ValueBool() {
		in["default"] = *v
	}
	var p client.Project
	if err := r.c.Do(ctx, "PATCH", client.Path("projects", st.ID.ValueString()), in, &p); err != nil {
		apiError(&resp.Diagnostics, "Changing the project", err)
		return
	}
	m.set(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Do(ctx, "DELETE", client.Path("projects", m.ID.ValueString()), nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Deleting the project (it must hold no workspaces)", err)
	}
}

// ImportState takes the project's id or name.
func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := r.importID(ctx, "/projects", req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Importing the project", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// data "ops_project": a project by name.

type projectDataSource struct{ base }

func newProjectDataSource() datasource.DataSource { return &projectDataSource{} }

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(req.ProviderData, &resp.Diagnostics)
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "A project, by name.",
		Attributes: map[string]dschema.Attribute{
			"name":        dschema.StringAttribute{Required: true},
			"id":          dschema.StringAttribute{Computed: true},
			"description": dschema.StringAttribute{Computed: true},
			"runner_pool": dschema.StringAttribute{Computed: true},
			"default":     dschema.BoolAttribute{Computed: true},
		},
	}
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := d.byName(ctx, "/projects", m.Name.ValueString())
	var p client.Project
	if err == nil {
		err = d.c.Do(ctx, "GET", client.Path("projects", id), nil, &p)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Reading the project", err)
		return
	}
	m.set(p)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
