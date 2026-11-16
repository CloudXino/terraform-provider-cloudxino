// Package provider is the CloudXino Terraform provider.
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

var _ provider.Provider = (*cloudxinoProvider)(nil)

type cloudxinoProvider struct {
	version string
}

type providerModel struct {
	Token  types.String `tfsdk:"token"`
	APIURL types.String `tfsdk:"api_url"`
}

// New returns the provider factory for providerserver.Serve.
func New(version string) func() provider.Provider {
	return func() provider.Provider { return &cloudxinoProvider{version: version} }
}

func (p *cloudxinoProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cloudxino"
	resp.Version = p.version
}

func (p *cloudxinoProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage CloudXino resources: CC2 servers, Cloud Firewalls, load balancers, reserved IPs, Kubernetes (CKS) clusters, DNS and SSH keys.",
		Attributes: map[string]schema.Attribute{
			"token": schema.StringAttribute{
				Description: "API key (Account → API keys in the console). Defaults to the CLOUDXINO_TOKEN environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"api_url": schema.StringAttribute{
				Description: "API address. Defaults to CLOUDXINO_API_URL, else " + cloudxino.DefaultBaseURL + ".",
				Optional:    true,
			},
		},
	}
}

func (p *cloudxinoProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if cfg.Token.IsUnknown() || cfg.APIURL.IsUnknown() {
		resp.Diagnostics.AddError("Provider configuration not known yet",
			"token and api_url must be known when the provider is configured.")
		return
	}
	token := os.Getenv("CLOUDXINO_TOKEN")
	if v := cfg.Token.ValueString(); v != "" {
		token = v
	}
	apiURL := os.Getenv("CLOUDXINO_API_URL")
	if v := cfg.APIURL.ValueString(); v != "" {
		apiURL = v
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(path.Root("token"), "Missing API key",
			"Set token in the provider block or the CLOUDXINO_TOKEN environment variable. Create a key under Account → API keys.")
		return
	}
	opts := []cloudxino.Option{cloudxino.WithUserAgent("terraform-provider-cloudxino/" + p.version)}
	if apiURL != "" {
		opts = append(opts, cloudxino.WithBaseURL(apiURL))
	}
	client, err := cloudxino.NewClient(token, opts...)
	if err != nil {
		resp.Diagnostics.AddError("Cannot create the CloudXino client", err.Error())
		return
	}
	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *cloudxinoProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newSSHKeyResource,
		newFirewallResource,
		newDNSZoneResource,
		newDNSRecordResource,
		newServerResource,
		newLoadBalancerResource,
		newReservedIPResource,
		newReservedIPAssignmentResource,
		newKubernetesClusterResource,
	}
}

func (p *cloudxinoProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newAccountDataSource,
		newProjectDataSource,
		newSSHKeyDataSource,
		newServerDataSource,
		newDNSZoneDataSource,
		newReservedIPDataSource,
		newKubernetesClusterDataSource,
	}
}
