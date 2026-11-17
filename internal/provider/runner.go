package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

// ops_runner: a runner registered in a pool, and its token (admins'). The token is what the runner starts with
// (OPS_RUNNER_TOKEN): created here and returned once, or one from a secret store, registered.

type runnerModel struct {
	ID    types.String `tfsdk:"id"`
	Name  types.String `tfsdk:"name"`
	Pool  types.String `tfsdk:"pool"`
	Token types.String `tfsdk:"token"`
}

type runnerResource struct{ base }

func newRunnerResource() resource.Resource { return &runnerResource{} }

func (r *runnerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_runner"
}

func (r *runnerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A runner: registered in a pool, with the token it starts with (OPS_RUNNER_TOKEN), e.g. for the Helm chart's secret. Runs of a project go only to runners of its pool. Deleting it revokes the token at once.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "2–63 lowercase letters, digits and dashes."},
			"pool": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("default"), PlanModifiers: replace,
				Description: "It gets the runs of projects that use this pool. Default: `default`."},
			"token": schema.StringAttribute{Optional: true, Computed: true, Sensitive: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplaceIfConfigured(), stringplanmodifier.UseStateForUnknown()},
				Description:   "Its token. Leave it out and OPS creates one (kept in Terraform's state: keep that encrypted); or set one from a secret store (at least 32 characters, e.g. `openssl rand -hex 32`)."},
		},
	}
}

func (r *runnerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m runnerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := map[string]any{"name": m.Name.ValueString(), "pool": m.Pool.ValueString()}
	if v := strPtr(m.Token); v != nil {
		in["token"] = *v
	}
	var rn client.Runner
	if err := r.c.Do(ctx, "POST", "/runners", in, &rn); err != nil {
		apiError(&resp.Diagnostics, "Registering the runner", err)
		return
	}
	m.ID, m.Pool = types.StringValue(rn.ID), types.StringValue(rn.Pool)
	if rn.Token != "" { // created here: the only time OPS returns it
		m.Token = types.StringValue(rn.Token)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *runnerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m runnerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var rn client.Runner
	if err := r.c.Do(ctx, "GET", client.Path("runners", m.ID.ValueString()), nil, &rn); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "Reading the runner", err)
		return
	}
	m.Name, m.Pool = types.StringValue(rn.Name), types.StringValue(rn.Pool) // the token stays: never returned
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Update can't happen: name, pool and token replace the runner.
func (r *runnerResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {}

func (r *runnerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m runnerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Do(ctx, "DELETE", client.Path("runners", m.ID.ValueString()), nil, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "Deleting the runner", err)
	}
}

// ImportState takes the runner's id or name. Its token isn't returned: an imported runner has none in the state.
func (r *runnerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := r.importID(ctx, "/runners", req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Importing the runner", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
}
