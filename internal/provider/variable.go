package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// ops_workspace_variable and ops_variable_set_variable: one variable of a workspace or of a variable set.
// A sensitive value is write-only in OPS: the provider keeps the configured value and can't see a change made in
// the UI.

// variableModel is a variable resource's attributes; owner is its workspace_id or variable_set_id. Read and written
// attribute by attribute (getVar, setVar): the owner attribute's name differs per resource.
type variableModel struct {
	ID, Owner, Category, Key, Value types.String
	Sensitive, HCL                  types.Bool
}

// attrGetter is a Plan's, State's or Config's GetAttribute.
type attrGetter func(context.Context, path.Path, any) diag.Diagnostics

func (r *variableResource) getVar(ctx context.Context, get attrGetter, diags *diag.Diagnostics) variableModel {
	var m variableModel
	for name, dst := range map[string]any{"id": &m.ID, r.ownerAttr: &m.Owner, "category": &m.Category, "key": &m.Key, "value": &m.Value,
		"sensitive": &m.Sensitive, "hcl": &m.HCL} {
		diags.Append(get(ctx, path.Root(name), dst)...)
	}
	return m
}

func (r *variableResource) setVar(ctx context.Context, st *tfsdk.State, m variableModel, diags *diag.Diagnostics) {
	for name, v := range map[string]any{"id": m.ID, r.ownerAttr: m.Owner, "category": m.Category, "key": m.Key, "value": m.Value,
		"sensitive": m.Sensitive, "hcl": m.HCL} {
		diags.Append(st.SetAttribute(ctx, path.Root(name), v)...)
	}
}

// variableResource is the variable resource of an owner kind: the API path of its owners and its attribute.
type variableResource struct {
	base
	kind      string // "workspaces" or "variable-sets"
	ownerAttr string // "workspace_id" or "variable_set_id"
	typeName  string // "_workspace_variable" or "_variable_set_variable"
	what      string // "workspace" or "variable set", for descriptions
}

func newWorkspaceVariableResource() resource.Resource {
	return &variableResource{kind: "workspaces", ownerAttr: "workspace_id", typeName: "_workspace_variable", what: "workspace"}
}

func newVariableSetVariableResource() resource.Resource {
	return &variableResource{kind: "variable-sets", ownerAttr: "variable_set_id", typeName: "_variable_set_variable", what: "variable set"}
}

func (r *variableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + r.typeName
}

func (r *variableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A variable of a " + r.what + ": an environment variable (e.g. cloud credentials) or a Terraform input variable. A sensitive value is write-only in OPS: Terraform can't see a change made to it in the UI.",
		Attributes: map[string]schema.Attribute{
			"id":        schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			r.ownerAttr: schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "The " + r.what + "'s id."},
			"category": schema.StringAttribute{Required: true, PlanModifiers: replace,
				Description: "`env` (an environment variable) or `terraform` (a Terraform input variable).",
				Validators:  []validator.String{stringvalidator.OneOf("env", "terraform")}},
			"key": schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "Its name."},
			"value": schema.StringAttribute{Required: true, Sensitive: true,
				Description: "Its value; with `hcl`, an HCL literal such as `{env = \"prd\"}` or `[\"a\", \"b\"]`."},
			"sensitive": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Write-only: never shown again, masked in run logs."},
			"hcl": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				Description: "Terraform variables: the value is HCL, for list, map, object, number or bool variables."},
		},
	}
}

func (r *variableResource) path(owner string, id ...string) string {
	return client.Path(append([]string{r.kind, owner, "variables"}, id...)...)
}

func (r *variableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	m := r.getVar(ctx, req.Plan.GetAttribute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var v client.Variable
	in := map[string]any{"category": m.Category.ValueString(), "key": m.Key.ValueString(), "value": m.Value.ValueString(),
		"sensitive": m.Sensitive.ValueBool(), "hcl": m.HCL.ValueBool()}
	if err := r.c.Do(ctx, "POST", r.path(m.Owner.ValueString()), in, &v); err != nil {
		apiError(&resp.Diagnostics, "Creating the variable", err)
		return
	}
	m.ID = types.StringValue(v.ID)
	r.setVar(ctx, &resp.State, m, &resp.Diagnostics)
}

func (r *variableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	m := r.getVar(ctx, req.State.GetAttribute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	var v client.Variable
	if err := r.c.Do(ctx, "GET", r.path(m.Owner.ValueString(), m.ID.ValueString()), nil, &v); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the variable", err)
		return
	}
	m.Category, m.Key, m.Sensitive, m.HCL = types.StringValue(v.Category), types.StringValue(v.Key), types.BoolValue(v.Sensitive), types.BoolValue(v.HCL)
	if v.Value != nil { // a sensitive one keeps the configured value: OPS doesn't return it
		m.Value = types.StringValue(*v.Value)
	} else if m.Value.IsNull() { // imported: unknown until the configuration sets it
		m.Value = types.StringValue("")
	}
	r.setVar(ctx, &resp.State, m, &resp.Diagnostics)
}

func (r *variableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	m := r.getVar(ctx, req.Plan.GetAttribute, &resp.Diagnostics)
	st := r.getVar(ctx, req.State.GetAttribute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	in := map[string]any{"value": m.Value.ValueString(), "sensitive": m.Sensitive.ValueBool(), "hcl": m.HCL.ValueBool()}
	if err := r.c.Do(ctx, "PATCH", r.path(st.Owner.ValueString(), st.ID.ValueString()), in, nil); err != nil {
		apiError(&resp.Diagnostics, "Changing the variable", err)
		return
	}
	m.ID = st.ID
	r.setVar(ctx, &resp.State, m, &resp.Diagnostics)
}

func (r *variableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	m := r.getVar(ctx, req.State.GetAttribute, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Do(ctx, "DELETE", r.path(m.Owner.ValueString(), m.ID.ValueString()), nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Deleting the variable", err)
	}
}

// ImportState takes "<owner id>/<variable id>". A sensitive variable's value isn't returned: set it in the
// configuration, and the next apply writes it.
func (r *variableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, err := splitID(req.ID, 2, "<"+r.ownerAttr+">/<variable id>")
	if err != nil {
		resp.Diagnostics.AddError("Importing the variable", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.ownerAttr), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), parts[1])...)
}
