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

// canonicalDomain refuses a domain the API would store differently (it lowercases, trims and drops
// a trailing dot), which would otherwise show as a change on every plan.
type canonicalDomainValidator struct{}

func canonicalDomain() validator.String { return canonicalDomainValidator{} }

func (canonicalDomainValidator) Description(context.Context) string {
	return "a lowercase domain name without a trailing dot"
}

func (v canonicalDomainValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (canonicalDomainValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	if want := strings.TrimRight(strings.ToLower(strings.TrimSpace(got)), "."); want != got {
		resp.Diagnostics.AddAttributeError(req.Path, "Invalid domain",
			fmt.Sprintf("write %q as %q (lowercase, no spaces or trailing dot)", got, want))
	}
}
