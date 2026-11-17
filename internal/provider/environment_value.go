package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// ops_environment_value: a project's value for a template's per-environment input (its Environment values
// page): every workspace of the project runs with it.

type environmentValueModel struct {
	ID         types.String `tfsdk:"id"`
	ProjectID  types.String `tfsdk:"project_id"`
	TemplateID types.String `tfsdk:"template_id"`
	Input      types.String `tfsdk:"input"`
	Value      types.String `tfsdk:"value"`
	KeepOwn    types.Bool   `tfsdk:"keep_own"`
}

type environmentValueResource struct{ base }

func newEnvironmentValueResource() resource.Resource { return &environmentValueResource{} }

func (r *environmentValueResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment_value"
}

func (r *environmentValueResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A project's value for a template's per-environment input (the project's Environment values): every workspace of the project runs with it, and its forms show it read-only. Admins and operators of the project.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, Description: "<project_id>/<template_id>/<input>.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project_id":  schema.StringAttribute{Required: true, PlanModifiers: replace},
			"template_id": schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "The template's id in the catalog."},
			"input":       schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "The input's name; the template marks it per environment."},
			"value": schema.StringAttribute{Required: true,
				Description: "As the input takes it: `7`, `true`, an option's value. Checked by the input's rules (type, options, pattern)."},
			"keep_own": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Workspaces with a value of their own keep it (one saved before, or what an import found); the others take this one."},
		},
	}
}

func (m *environmentValueModel) path() string {
	return client.Path("projects", m.ProjectID.ValueString(), "environment", m.TemplateID.ValueString(), m.Input.ValueString())
}

func (r *environmentValueResource) put(ctx context.Context, m *environmentValueModel) error {
	var v client.EnvironmentValue
	if err := r.c.Do(ctx, "PUT", m.path(), map[string]any{"value": m.Value.ValueString(), "keep_own": m.KeepOwn.ValueBool()}, &v); err != nil {
		return err
	}
	m.ID = types.StringValue(m.ProjectID.ValueString() + "/" + m.TemplateID.ValueString() + "/" + m.Input.ValueString())
	m.Value, m.KeepOwn = types.StringValue(v.Value), types.BoolValue(v.KeepOwn)
	return nil
}

func (r *environmentValueResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m environmentValueModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.put(ctx, &m); err != nil {
		apiError(&resp.Diagnostics, "Setting the environment value", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *environmentValueResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m environmentValueModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var list []client.EnvironmentValue
	if err := r.c.Do(ctx, "GET", client.Path("projects", m.ProjectID.ValueString(), "environment"), nil, &list); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the environment values", err)
		return
	}
	for _, v := range list {
		if v.TemplateID == m.TemplateID.ValueString() && v.Input == m.Input.ValueString() {
			m.Value, m.KeepOwn = types.StringValue(v.Value), types.BoolValue(v.KeepOwn)
			m.ID = types.StringValue(m.ProjectID.ValueString() + "/" + v.TemplateID + "/" + v.Input)
			resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
			return
		}
	}
	resp.State.RemoveResource(ctx) // cleared in the UI
}

func (r *environmentValueResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m environmentValueModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.put(ctx, &m); err != nil {
		apiError(&resp.Diagnostics, "Setting the environment value", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *environmentValueResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m environmentValueModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Do(ctx, "DELETE", m.path(), nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Clearing the environment value", err)
	}
}

// ImportState takes "<project_id>/<template_id>/<input>".
func (r *environmentValueResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitID(req.ID, 3, "<project_id>/<template_id>/<input>")
	if err != nil {
		resp.Diagnostics.AddError("Importing the environment value", err.Error())
		return
	}
	for i, name := range []string{"project_id", "template_id", "input"} {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(name), parts[i])...)
	}
}
