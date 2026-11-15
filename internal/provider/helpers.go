package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

// clientFrom takes the client the provider configured (nil while Terraform validates offline).
func clientFrom(data any, diags *diag.Diagnostics) *cloudxino.Client {
	if data == nil {
		return nil
	}
	c, ok := data.(*cloudxino.Client)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("expected *cloudxino.Client, got %T", data))
		return nil
	}
	return c
}

type resourceBase struct{ client *cloudxino.Client }

func (b *resourceBase) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	b.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

type dataSourceBase struct{ client *cloudxino.Client }

func (b *dataSourceBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	b.client = clientFrom(req.ProviderData, &resp.Diagnostics)
}

// apiError turns an SDK error into a diagnostic with the API's message.
func apiError(diags *diag.Diagnostics, action string, err error) {
	var apiErr *cloudxino.APIError
	if errors.As(err, &apiErr) {
		diags.AddError(fmt.Sprintf("Cannot %s", action), fmt.Sprintf("%s (%d %s)", apiErr.Message, apiErr.StatusCode, apiErr.Code))
		return
	}
	diags.AddError(fmt.Sprintf("Cannot %s", action), err.Error())
}

// optString is nil for a null or unknown value.
func optString(v types.String) *string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	s := v.ValueString()
	return &s
}

func optInt(v types.Int64) *int {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	i := int(v.ValueInt64())
	return &i
}

func strValue(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// strValueOrEmpty: an API null becomes "" (for attributes the API always has, like a name).
func strValueOrEmpty(s *string) types.String {
	if s == nil {
		return types.StringValue("")
	}
	return types.StringValue(*s)
}

func intValue(i *int) types.Int64 {
	if i == nil {
		return types.Int64Null()
	}
	return types.Int64Value(int64(*i))
}

func stringsFromSet(ctx context.Context, v types.Set, diags *diag.Diagnostics) []string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	out := []string{}
	diags.Append(v.ElementsAs(ctx, &out, false)...)
	return out
}

func stringsFromList(ctx context.Context, v types.List, diags *diag.Diagnostics) []string {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	out := []string{}
	diags.Append(v.ElementsAs(ctx, &out, false)...)
	return out
}

func setOf(items *[]string) types.Set {
	vals := []attr.Value{}
	if items != nil {
		for _, s := range *items {
			vals = append(vals, types.StringValue(s))
		}
	}
	return types.SetValueMust(types.StringType, vals)
}

func listOf(items *[]string) types.List {
	vals := []attr.Value{}
	if items != nil {
		for _, s := range *items {
			vals = append(vals, types.StringValue(s))
		}
	}
	return types.ListValueMust(types.StringType, vals)
}

func ptr[T any](v T) *T { return &v }
