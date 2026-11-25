package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

// --- cloudxino_container_registry ----------------------------------------------------------------

type containerRegistryDataSource struct{ dataSourceBase }

type containerRegistryModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Endpoint  types.String `tfsdk:"endpoint"`
	Region    types.String `tfsdk:"region"`
	Status    types.String `tfsdk:"status"`
	ProjectID types.String `tfsdk:"project_id"`
}

func newContainerRegistryDataSource() datasource.DataSource { return &containerRegistryDataSource{} }

func (d *containerRegistryDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_registry"
}

func (d *containerRegistryDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Container Hub (CCH) registry of the team, by `id` or `name`. Registries are made in the console; " +
			"give its `id` to a `cloudxino_container_host`'s `registry_ids` to pull private images from it.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Optional: true, Computed: true},
			"name":       schema.StringAttribute{Optional: true, Computed: true},
			"endpoint":   schema.StringAttribute{Computed: true, Description: "Image prefix: push and pull `<endpoint>/<image>:<tag>`."},
			"region":     schema.StringAttribute{Computed: true},
			"status":     schema.StringAttribute{Computed: true, Description: "`active` once the registry can be used."},
			"project_id": schema.StringAttribute{Computed: true},
		},
	}
}

func (d *containerRegistryDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m containerRegistryModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.ID.IsNull() == m.Name.IsNull() {
		resp.Diagnostics.AddError("Set id or name", "Give exactly one of `id` and `name`.")
		return
	}
	regs, err := d.client.Registries.List(ctx, nil)
	if err != nil {
		apiError(&resp.Diagnostics, "list Container Hub registries", err)
		return
	}
	var found *cloudxino.ContainerRegistry
	for i := range regs {
		if (!m.ID.IsNull() && regs[i].Id == m.ID.ValueString()) || (!m.Name.IsNull() && regs[i].Name == m.Name.ValueString()) {
			found = &regs[i]
			break
		}
	}
	if found == nil {
		resp.Diagnostics.AddError("Container Hub registry not found", fmt.Sprintf("No registry of the team is called %q.", m.Name.ValueString()+m.ID.ValueString()))
		return
	}
	m.ID, m.Name, m.Endpoint = types.StringValue(found.Id), types.StringValue(found.Name), types.StringValue(found.Endpoint)
	m.Region, m.Status, m.ProjectID = types.StringValue(found.Region), types.StringValue(found.Status), strValue(found.ProjectId)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
