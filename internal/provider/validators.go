package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

type oneOfValidator struct{ allowed []string }

func oneOf(allowed ...string) validator.String { return oneOfValidator{allowed} }

func (v oneOfValidator) Description(context.Context) string {
	return "one of: " + strings.Join(v.allowed, ", ")
}

func (v oneOfValidator) MarkdownDescription(ctx context.Context) string { return v.Description(ctx) }

func (v oneOfValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	for _, a := range v.allowed {
		if got == a {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid value",
		fmt.Sprintf("%q is not allowed; use one of: %s", got, strings.Join(v.allowed, ", ")))
}
