package provider

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/ops-plane/terraform-provider-ops/internal/client"
)

func pathRoot(name string) path.Path { return path.Root(name) }

// base is what every resource and data source embeds: the configured client.
type base struct{ c *client.Client }

func (b *base) configure(data any, diags *diag.Diagnostics) {
	if data == nil { // before the provider is configured (validation)
		return
	}
	c, ok := data.(*client.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("got %T", data))
		return
	}
	b.c = c
}

func (b *base) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	b.configure(req.ProviderData, &resp.Diagnostics)
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// byName finds the object of kind (the list path: "/projects") named name and returns its id; an error if none.
func (b *base) byName(ctx context.Context, kind, name string) (string, error) {
	var list []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := b.c.Do(ctx, "GET", kind+"?name="+urlQuery(name), nil, &list); err != nil {
		return "", err
	}
	for _, o := range list {
		if o.Name == name {
			return o.ID, nil
		}
	}
	return "", fmt.Errorf("no %s named %q (or the token may not see it)", strings.TrimPrefix(kind, "/"), name)
}

// importID is an import's id: as given if it's an id, else the id of the object of kind named so.
func (b *base) importID(ctx context.Context, kind, given string) (string, error) {
	if uuidRe.MatchString(given) {
		return given, nil
	}
	return b.byName(ctx, kind, given)
}

func urlQuery(s string) string { return url.QueryEscape(s) }

// apiError adds err to diags as what failed; a 422's fields are in its text.
func apiError(diags *diag.Diagnostics, what string, err error) {
	diags.AddError(what, err.Error())
}

// strPtr is v's value, nil when null or unknown: a field PATCH leaves alone.
func strPtr(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func boolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	b := v.ValueBool()
	return &b
}

// stringsOf is a set of strings as a slice; nil when null or unknown.
func stringsOf(ctx context.Context, s types.Set, diags *diag.Diagnostics) *[]string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	out := []string{}
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	return &out
}

func setOf(ctx context.Context, items []string, diags *diag.Diagnostics) types.Set {
	if items == nil {
		items = []string{}
	}
	s, d := types.SetValueFrom(ctx, types.StringType, items)
	diags.Append(d...)
	return s
}

// splitID splits a composite import id "a/b[/c]" into n parts; an error otherwise.
func splitID(id string, n int, form string) ([]string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != n {
		return nil, fmt.Errorf("import id %q: want %s", id, form)
	}
	return parts, nil
}
