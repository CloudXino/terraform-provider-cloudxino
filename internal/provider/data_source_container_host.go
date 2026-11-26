package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// --- cloudxino_container_host --------------------------------------------------------------------

type containerHostDataSource struct{ dataSourceBase }

type containerHostDSModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Plan           types.String `tfsdk:"plan"`
	Region         types.String `tfsdk:"region"`
	ProjectID      types.String `tfsdk:"project_id"`
	Status         types.String `tfsdk:"status"`
	IPType         types.String `tfsdk:"ip_type"`
	IPAddress      types.String `tfsdk:"ip_address"`
	IngressIP      types.String `tfsdk:"ingress_ip"`
	PortainerURL   types.String `tfsdk:"portainer_url"`
	Compose        types.String `tfsdk:"compose"`
	EnvKeys        types.List   `tfsdk:"env_keys"`
	Domains        types.List   `tfsdk:"domains"`
	LastDeployment types.Object `tfsdk:"last_deployment"`
}

func newContainerHostDataSource() datasource.DataSource { return &containerHostDataSource{} }

func (d *containerHostDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_host"
}

func (d *containerHostDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Managed Container Service host of the team, by `name` or `id` (for hosts made outside this configuration).",
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Optional: true, Computed: true},
			"name":          schema.StringAttribute{Optional: true, Computed: true},
			"plan":          schema.StringAttribute{Computed: true, Description: "Plan slug (a CC2 size)."},
			"region":        schema.StringAttribute{Computed: true},
			"project_id":    schema.StringAttribute{Computed: true},
			"status":        schema.StringAttribute{Computed: true, Description: "`creating`, `installing`, `ready`, `failed`, `deleting` or `deleted`."},
			"ip_type":       schema.StringAttribute{Computed: true, Description: "`public` or `nat`."},
			"ip_address":    schema.StringAttribute{Computed: true, Description: "The server's IPv4 address (172.16.x for a NAT host)."},
			"ingress_ip":    schema.StringAttribute{Computed: true, Description: "Point domains' A records here: the server's IP, or the NAT web proxy's for a NAT host."},
			"portainer_url": schema.StringAttribute{Computed: true},
			"compose":       schema.StringAttribute{Computed: true, Description: "The saved `docker-compose.yml`; null before the first."},
			"env_keys": schema.ListAttribute{
				ElementType: types.StringType, Computed: true,
				Description: "Names of the saved environment variables (not their values).",
			},
			"domains": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"domain":  schema.StringAttribute{Computed: true},
					"service": schema.StringAttribute{Computed: true},
					"port":    schema.Int64Attribute{Computed: true},
				}},
			},
			"last_deployment": schema.SingleNestedAttribute{
				Computed: true, Description: "The newest deployment; null before the first.",
				Attributes: map[string]schema.Attribute{
					"id":     schema.StringAttribute{Computed: true},
					"number": schema.Int64Attribute{Computed: true},
					"status": schema.StringAttribute{Computed: true, Description: "`running`, `succeeded` or `failed`."},
				},
			},
		},
	}
}

func (d *containerHostDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m containerHostDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	if id == "" {
		if m.Name.IsNull() {
			resp.Diagnostics.AddError("Which container host?", "Set name or id.")
			return
		}
		list, err := d.client.ContainerHosts.List(ctx, nil)
		if err != nil {
			apiError(&resp.Diagnostics, "list container hosts", err)
			return
		}
		var found []string
		for _, h := range list {
			if h.Name == m.Name.ValueString() {
				found = append(found, h.Id)
			}
		}
		if len(found) != 1 {
			resp.Diagnostics.AddError("Container host not found",
				fmt.Sprintf("The team has %d container hosts named %q; use id to pick one.", len(found), m.Name.ValueString()))
			return
		}
		id = found[0]
	}
	h, err := d.client.ContainerHosts.Get(ctx, id)
	if err != nil {
		apiError(&resp.Diagnostics, "read the container host", err)
		return
	}
	m.ID, m.Name, m.Plan = types.StringValue(h.Id), types.StringValue(h.Name), types.StringValue(h.Plan)
	m.Region, m.ProjectID, m.Status = types.StringValue(h.Server.Region), types.StringValue(h.ProjectId), types.StringValue(h.Status)
	m.IPAddress, m.PortainerURL = types.StringValue(h.Server.IpAddress), types.StringValue(h.PortainerUrl)
	m.IPType, m.IngressIP = types.StringValue(ipTypeOf(h)), types.StringValue(ingressIPOf(h))
	m.Compose = strValue(h.Compose)
	m.EnvKeys = listOf(&h.EnvKeys)
	m.Domains = domainsList(h.Domains)
	m.LastDeployment = deploymentObject(h.LastDeployment)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// --- cloudxino_container_host_plans --------------------------------------------------------------

type containerHostPlansDataSource struct{ dataSourceBase }

type containerHostPlansModel struct {
	Enabled types.Bool           `tfsdk:"enabled"`
	Plans   []containerPlanModel `tfsdk:"plans"`
}

type containerPlanModel struct {
	Slug           types.String  `tfsdk:"slug"`
	CPU            types.Int64   `tfsdk:"cpu"`
	MemoryGB       types.Float64 `tfsdk:"memory_gb"`
	DiskGB         types.Int64   `tfsdk:"disk_gb"`
	ServerPerMonth types.Float64 `tfsdk:"server_per_month"`
	FeePerMonth    types.Float64 `tfsdk:"fee_per_month"`
	PerMonth       types.Float64 `tfsdk:"per_month"`
	PerHour        types.Float64 `tfsdk:"per_hour"`
	NatPerMonth    types.Float64 `tfsdk:"nat_per_month"`
}

func newContainerHostPlansDataSource() datasource.DataSource { return &containerHostPlansDataSource{} }

func (d *containerHostPlansDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_host_plans"
}

func (d *containerHostPlansDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "The Managed Container Service plans a new `cloudxino_container_host` can use, cheapest first, with monthly prices in BDT.",
		Attributes: map[string]schema.Attribute{
			"enabled": schema.BoolAttribute{Computed: true, Description: "Whether new hosts can be ordered right now."},
			"plans": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"slug":             schema.StringAttribute{Computed: true, Description: "The value for `plan`, e.g. `s-2vcpu-4gb`."},
					"cpu":              schema.Int64Attribute{Computed: true},
					"memory_gb":        schema.Float64Attribute{Computed: true},
					"disk_gb":          schema.Int64Attribute{Computed: true},
					"server_per_month": schema.Float64Attribute{Computed: true, Description: "The server's price (BDT)."},
					"fee_per_month":    schema.Float64Attribute{Computed: true, Description: "The management fee (BDT)."},
					"per_month":        schema.Float64Attribute{Computed: true, Description: "Server plus fee (BDT)."},
					"per_hour":         schema.Float64Attribute{Computed: true, Description: "Server plus fee per hour (BDT)."},
					"nat_per_month": schema.Float64Attribute{Computed: true,
						Description: "Server plus fee for a NAT host (`ip_type = \"nat\"`, no public IP) (BDT); 0 when the API does not say."},
				}},
			},
		},
	}
}

func (d *containerHostPlansDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	o, err := d.client.ContainerHosts.Options(ctx)
	if err != nil {
		apiError(&resp.Diagnostics, "read the container host plans", err)
		return
	}
	m := containerHostPlansModel{Enabled: types.BoolValue(o.Enabled), Plans: []containerPlanModel{}}
	for _, p := range o.Plans {
		m.Plans = append(m.Plans, containerPlanModel{
			Slug: types.StringValue(p.Slug), CPU: types.Int64Value(int64(p.Cpu)), MemoryGB: types.Float64Value(p.MemoryGb),
			DiskGB: types.Int64Value(int64(p.DiskGb)), ServerPerMonth: types.Float64Value(p.ServerPerMonth),
			FeePerMonth: types.Float64Value(p.FeePerMonth), PerMonth: types.Float64Value(p.PerMonth), PerHour: types.Float64Value(p.PerHour),
			NatPerMonth: types.Float64Value(p.NatPerMonth),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
