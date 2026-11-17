package provider

import (
	"context"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// ops_variable_set: variables shared by many workspaces (admins'). Where it applies: global, or attached to
// projects and workspaces (ops_variable_set_project, ops_variable_set_workspace).

type variableSetModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Global      types.Bool   `tfsdk:"global"`
	Priority    types.Bool   `tfsdk:"priority"`
}

func (m *variableSetModel) set(v client.VariableSet) {
	m.ID, m.Name, m.Description = types.StringValue(v.ID), types.StringValue(v.Name), types.StringValue(v.Description)
	m.Global, m.Priority = types.BoolValue(v.Global), types.BoolValue(v.Priority)
}

type variableSetResource struct{ base }

func newVariableSetResource() resource.Resource { return &variableSetResource{} }

func (r *variableSetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_variable_set"
}

func (r *variableSetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A variable set: variables (ops_variable_set_variable) shared by many workspaces, such as a subscription's credentials. It applies globally, or where it's attached (ops_variable_set_project, ops_variable_set_workspace). Repository-linked workspaces get only sets attached to them.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description: "Its name; changing it replaces the set."},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"global": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Applies to every workspace that isn't linked to a repository."},
			"priority": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Its variables win over the workspace's own and other sets': for values an admin enforces."},
		},
	}
}

func (r *variableSetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m variableSetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var v client.VariableSet
	in := map[string]any{"name": m.Name.ValueString(), "description": m.Description.ValueString(), "global": m.Global.ValueBool(), "priority": m.Priority.ValueBool()}
	if err := r.c.Do(ctx, "POST", "/variable-sets", in, &v); err != nil {
		apiError(&resp.Diagnostics, "Creating the variable set", err)
		return
	}
	m.set(v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *variableSetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m variableSetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var v client.VariableSet
	if err := r.c.Do(ctx, "GET", client.Path("variable-sets", m.ID.ValueString()), nil, &v); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the variable set", err)
		return
	}
	m.set(v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *variableSetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m variableSetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var v client.VariableSet
	in := map[string]any{"description": m.Description.ValueString(), "global": m.Global.ValueBool(), "priority": m.Priority.ValueBool()}
	if err := r.c.Do(ctx, "PATCH", client.Path("variable-sets", m.ID.ValueString()), in, &v); err != nil {
		apiError(&resp.Diagnostics, "Changing the variable set", err)
		return
	}
	m.set(v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *variableSetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m variableSetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Do(ctx, "DELETE", client.Path("variable-sets", m.ID.ValueString()), nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Deleting the variable set", err)
	}
}

// ImportState takes the set's id or name.
func (r *variableSetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := r.importID(ctx, "/variable-sets", req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Importing the variable set", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}

// ops_variable_set_project and ops_variable_set_workspace: a set applied to one project's workspaces or to
// one workspace. Its own resource, so a workspace's configuration can attach a set without owning the set's list.

type attachmentResource struct {
	base
	project  bool   // a project's workspaces; else one workspace
	attr     string // "project_id" or "workspace_id"
	segment  string // the API path segment: "projects" or "workspaces"
	typeName string
}

func newVariableSetProjectResource() resource.Resource {
	return &attachmentResource{project: true, attr: "project_id", segment: "projects", typeName: "_variable_set_project"}
}

func newVariableSetWorkspaceResource() resource.Resource {
	return &attachmentResource{attr: "workspace_id", segment: "workspaces", typeName: "_variable_set_workspace"}
}

func (r *attachmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + r.typeName
}

func (r *attachmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	what := "a workspace (not global or project sets: a repository-linked workspace gets only sets attached to it)"
	if r.project {
		what = "the workspaces of a project (not those linked to a repository)"
	}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Applies a variable set to " + what + ".",
		Attributes: map[string]schema.Attribute{
			"id":              schema.StringAttribute{Computed: true, Description: "<variable_set_id>/<" + r.attr + ">.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"variable_set_id": schema.StringAttribute{Required: true, PlanModifiers: replace},
			r.attr:            schema.StringAttribute{Required: true, PlanModifiers: replace},
		},
	}
}

func (r *attachmentResource) get(ctx context.Context, get attrGetter, diags *diag.Diagnostics) (set, target string) {
	var s, t types.String
	diags.Append(get(ctx, path.Root("variable_set_id"), &s)...)
	diags.Append(get(ctx, path.Root(r.attr), &t)...)
	return s.ValueString(), t.ValueString()
}

func (r *attachmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	set, target := r.get(ctx, req.Plan.GetAttribute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Do(ctx, "PUT", client.Path("variable-sets", set, r.segment, target), nil, nil); err != nil {
		apiError(&resp.Diagnostics, "Attaching the variable set", err)
		return
	}
	r.save(ctx, &resp.State, set, target, &resp.Diagnostics)
}

func (r *attachmentResource) save(ctx context.Context, st *tfsdk.State, set, target string, diags *diag.Diagnostics) {
	diags.Append(st.SetAttribute(ctx, path.Root("id"), set+"/"+target)...)
	diags.Append(st.SetAttribute(ctx, path.Root("variable_set_id"), set)...)
	diags.Append(st.SetAttribute(ctx, path.Root(r.attr), target)...)
}

func (r *attachmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	set, target := r.get(ctx, req.State.GetAttribute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var v client.VariableSet
	if err := r.c.Do(ctx, "GET", client.Path("variable-sets", set), nil, &v); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the variable set", err)
		return
	}
	ids := v.WorkspaceIDs
	if r.project {
		ids = v.ProjectIDs
	}
	if !slices.Contains(ids, target) { // detached in the UI
		resp.State.RemoveResource(ctx)
		return
	}
	r.save(ctx, &resp.State, set, target, &resp.Diagnostics)
}

// Update can't happen: both attributes replace.
func (r *attachmentResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {
}

func (r *attachmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	set, target := r.get(ctx, req.State.GetAttribute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Do(ctx, "DELETE", client.Path("variable-sets", set, r.segment, target), nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Detaching the variable set", err)
	}
}

// ImportState takes "<variable_set_id>/<project_id or workspace_id>".
func (r *attachmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitID(req.ID, 2, "<variable_set_id>/<"+r.attr+">")
	if err != nil {
		resp.Diagnostics.AddError("Importing the attachment", err.Error())
		return
	}
	r.save(ctx, &resp.State, parts[0], parts[1], &resp.Diagnostics)
}

// data "ops_variable_set": a variable set by name.

type variableSetDataSource struct{ base }

func newVariableSetDataSource() datasource.DataSource { return &variableSetDataSource{} }

func (d *variableSetDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_variable_set"
}

func (d *variableSetDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.configure(req.ProviderData, &resp.Diagnostics)
}

func (d *variableSetDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "A variable set, by name.",
		Attributes: map[string]dschema.Attribute{
			"name":        dschema.StringAttribute{Required: true},
			"id":          dschema.StringAttribute{Computed: true},
			"description": dschema.StringAttribute{Computed: true},
			"global":      dschema.BoolAttribute{Computed: true},
			"priority":    dschema.BoolAttribute{Computed: true},
		},
	}
}

func (d *variableSetDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m variableSetModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := d.byName(ctx, "/variable-sets", m.Name.ValueString())
	var v client.VariableSet
	if err == nil {
		err = d.c.Do(ctx, "GET", client.Path("variable-sets", id), nil, &v)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "Reading the variable set", err)
		return
	}
	m.set(v)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
