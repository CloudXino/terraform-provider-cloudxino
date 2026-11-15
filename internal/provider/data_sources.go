package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

// --- cloudxino_account ---------------------------------------------------------------------------

type accountDataSource struct{ dataSourceBase }

type accountModel struct {
	Email            types.String `tfsdk:"email"`
	OrganizationID   types.String `tfsdk:"organization_id"`
	OrganizationName types.String `tfsdk:"organization_name"`
	MemberRole       types.String `tfsdk:"member_role"`
	KeyName          types.String `tfsdk:"api_key_name"`
	KeyRole          types.String `tfsdk:"api_key_role"`
}

func newAccountDataSource() datasource.DataSource { return &accountDataSource{} }

func (d *accountDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account"
}

func (d *accountDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Who the provider's API key acts for: the team and the key's role.",
		Attributes: map[string]schema.Attribute{
			"email":             schema.StringAttribute{Computed: true, Description: "The key's creator."},
			"organization_id":   schema.StringAttribute{Computed: true},
			"organization_name": schema.StringAttribute{Computed: true},
			"member_role":       schema.StringAttribute{Computed: true, Description: "The creator's role in the team."},
			"api_key_name":      schema.StringAttribute{Computed: true},
			"api_key_role":      schema.StringAttribute{Computed: true, Description: "What the key may do: admin, developer, billing or viewer."},
		},
	}
}

func (d *accountDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	me, err := d.client.Account.Me(ctx)
	if err != nil {
		apiError(&resp.Diagnostics, "read the account", err)
		return
	}
	m := accountModel{Email: types.StringValue(me.Email)}
	if o := me.Organization; o != nil {
		m.OrganizationID, m.OrganizationName, m.MemberRole = types.StringValue(o.ID), types.StringValue(o.Name), types.StringValue(o.Role)
	}
	if k := me.APIKey; k != nil {
		m.KeyName, m.KeyRole = types.StringValue(k.Name), types.StringValue(k.Role)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// --- cloudxino_project ---------------------------------------------------------------------------

type projectDataSource struct{ dataSourceBase }

type projectModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Slug        types.String `tfsdk:"slug"`
	Description types.String `tfsdk:"description"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
}

func newProjectDataSource() datasource.DataSource { return &projectDataSource{} }

func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A project of the team, by `id` or `name`; with neither, the default project.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Optional: true, Computed: true},
			"name":        schema.StringAttribute{Optional: true, Computed: true},
			"slug":        schema.StringAttribute{Computed: true},
			"description": schema.StringAttribute{Computed: true},
			"is_default":  schema.BoolAttribute{Computed: true},
		},
	}
}

func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	projects, err := d.client.Projects.List(ctx)
	if err != nil {
		apiError(&resp.Diagnostics, "list projects", err)
		return
	}
	var found *cloudxino.Project
	for i := range projects {
		pr := &projects[i]
		switch {
		case !m.ID.IsNull():
			if pr.Id == m.ID.ValueString() {
				found = pr
			}
		case !m.Name.IsNull():
			if pr.Name != nil && *pr.Name == m.Name.ValueString() {
				found = pr
			}
		default:
			if pr.IsDefault != nil && *pr.IsDefault {
				found = pr
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		resp.Diagnostics.AddError("Project not found", "No project of the team matches.")
		return
	}
	m.ID, m.Name, m.Slug, m.Description = types.StringValue(found.Id), strValueOrEmpty(found.Name), strValue(found.Slug), strValue(found.Description)
	m.IsDefault = types.BoolValue(found.IsDefault != nil && *found.IsDefault)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// --- cloudxino_ssh_key ---------------------------------------------------------------------------

type sshKeyDataSource struct{ dataSourceBase }

func newSSHKeyDataSource() datasource.DataSource { return &sshKeyDataSource{} }

func (d *sshKeyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (d *sshKeyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A saved SSH key, by `id`, `name` or `fingerprint`.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Optional: true, Computed: true},
			"name":        schema.StringAttribute{Optional: true, Computed: true},
			"fingerprint": schema.StringAttribute{Optional: true, Computed: true},
			"public_key":  schema.StringAttribute{Computed: true},
		},
	}
}

func (d *sshKeyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m sshKeyModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var key *cloudxino.SSHKeyOut
	if !m.ID.IsNull() {
		k, err := d.client.SSHKeys.Get(ctx, m.ID.ValueString())
		if err != nil {
			apiError(&resp.Diagnostics, "read the SSH key", err)
			return
		}
		key = k
	} else {
		if m.Name.IsNull() && m.Fingerprint.IsNull() {
			resp.Diagnostics.AddError("Which key?", "Set id, name or fingerprint.")
			return
		}
		keys, err := d.client.SSHKeys.List(ctx, &cloudxino.ListSSHKeysOptions{Name: m.Name.ValueString(), Fingerprint: m.Fingerprint.ValueString()})
		if err != nil {
			apiError(&resp.Diagnostics, "list SSH keys", err)
			return
		}
		if len(keys) != 1 {
			resp.Diagnostics.AddError("SSH key not found", fmt.Sprintf("%d keys match; want exactly 1.", len(keys)))
			return
		}
		key = &keys[0]
	}
	m.PublicKey = types.StringNull()
	m.fill(key)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// --- cloudxino_server ----------------------------------------------------------------------------

type serverDataSource struct{ dataSourceBase }

type serverDSModel struct {
	ID        types.String `tfsdk:"id"`
	Name      types.String `tfsdk:"name"`
	Image     types.String `tfsdk:"image"`
	Size      types.String `tfsdk:"size"`
	Region    types.String `tfsdk:"region"`
	Status    types.String `tfsdk:"status"`
	IPAddress types.String `tfsdk:"ip_address"`
	PrivateIP types.String `tfsdk:"private_ip"`
	IPType    types.String `tfsdk:"ip_type"`
	ProjectID types.String `tfsdk:"project_id"`
	Tags      types.Set    `tfsdk:"tags"`
}

func newServerDataSource() datasource.DataSource { return &serverDataSource{} }

func (d *serverDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (d *serverDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	c := func() schema.StringAttribute { return schema.StringAttribute{Computed: true} }
	resp.Schema = schema.Schema{
		Description: "A CC2 server, by `id` or by its unique `name`.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Optional: true, Computed: true},
			"name":       schema.StringAttribute{Optional: true, Computed: true},
			"image":      c(),
			"size":       c(),
			"region":     c(),
			"status":     c(),
			"ip_address": c(),
			"private_ip": c(),
			"ip_type":    c(),
			"project_id": c(),
			"tags":       schema.SetAttribute{Computed: true, ElementType: types.StringType},
		},
	}
}

func (d *serverDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m serverDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var srv *cloudxino.Server
	if !m.ID.IsNull() {
		s, err := d.client.Servers.Get(ctx, m.ID.ValueString())
		if err != nil {
			apiError(&resp.Diagnostics, "read the server", err)
			return
		}
		srv = s
	} else if !m.Name.IsNull() {
		list, err := d.client.Servers.List(ctx, nil)
		if err != nil {
			apiError(&resp.Diagnostics, "list servers", err)
			return
		}
		var hits []cloudxino.Server
		for _, s := range list {
			if s.Name != nil && strings.EqualFold(*s.Name, m.Name.ValueString()) {
				hits = append(hits, s)
			}
		}
		if len(hits) != 1 {
			resp.Diagnostics.AddError("Server not found", fmt.Sprintf("%d servers are named %q; want exactly 1.", len(hits), m.Name.ValueString()))
			return
		}
		srv = &hits[0]
	} else {
		resp.Diagnostics.AddError("Which server?", "Set id or name.")
		return
	}
	m.ID, m.Name = types.StringValue(srv.Id), strValueOrEmpty(srv.Name)
	m.Image, m.Size, m.Region = strValue(srv.Image), strValue(srv.Size), strValue(srv.Region)
	m.Status, m.IPAddress, m.PrivateIP, m.IPType = strValue(srv.Status), strValue(srv.IpAddress), strValue(srv.PrivateIp), strValue(srv.IpType)
	m.ProjectID, m.Tags = strValue(srv.ProjectId), setOf(srv.Tags)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// --- cloudxino_dns_zone --------------------------------------------------------------------------

type dnsZoneDataSource struct{ dataSourceBase }

func newDNSZoneDataSource() datasource.DataSource { return &dnsZoneDataSource{} }

func (d *dnsZoneDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (d *dnsZoneDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A hosted DNS zone, by `id` or `name`.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Optional: true, Computed: true},
			"name":        schema.StringAttribute{Optional: true, Computed: true},
			"status":      schema.StringAttribute{Computed: true},
			"nameservers": schema.ListAttribute{Computed: true, ElementType: types.StringType},
		},
	}
}

func (d *dnsZoneDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m dnsZoneModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	if m.ID.IsNull() {
		if m.Name.IsNull() {
			resp.Diagnostics.AddError("Which zone?", "Set id or name.")
			return
		}
		zones, err := d.client.DNS.ListZones(ctx)
		if err != nil {
			apiError(&resp.Diagnostics, "list DNS zones", err)
			return
		}
		want := strings.ToLower(strings.TrimSuffix(m.Name.ValueString(), "."))
		for _, z := range zones {
			if z.Name != nil && *z.Name == want {
				id = z.Id
			}
		}
		if id == "" {
			resp.Diagnostics.AddError("DNS zone not found", fmt.Sprintf("The team has no zone %q.", want))
			return
		}
	}
	z, err := d.client.DNS.GetZone(ctx, id)
	if err != nil {
		apiError(&resp.Diagnostics, "read the DNS zone", err)
		return
	}
	m.Name = types.StringNull()
	m.fill(z)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// --- cloudxino_reserved_ip -----------------------------------------------------------------------

type reservedIPDataSource struct{ dataSourceBase }

type reservedIPDSModel struct {
	ID        types.String `tfsdk:"id"`
	IPAddress types.String `tfsdk:"ip_address"`
	Region    types.String `tfsdk:"region"`
	ProjectID types.String `tfsdk:"project_id"`
	ServerID  types.String `tfsdk:"server_id"`
	Status    types.String `tfsdk:"status"`
}

func newReservedIPDataSource() datasource.DataSource { return &reservedIPDataSource{} }

func (d *reservedIPDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_reserved_ip"
}

func (d *reservedIPDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A reserved IP of the team, by `ip_address` or `id`.",
		Attributes: map[string]schema.Attribute{
			"id":         schema.StringAttribute{Optional: true, Computed: true},
			"ip_address": schema.StringAttribute{Optional: true, Computed: true, Description: "The address, e.g. `203.0.113.10`."},
			"region":     schema.StringAttribute{Computed: true},
			"project_id": schema.StringAttribute{Computed: true},
			"server_id":  schema.StringAttribute{Computed: true, Description: "The CC2 server it points at; null when unassigned."},
			"status":     schema.StringAttribute{Computed: true, Description: "`available`, `assigned` or `deleting`."},
		},
	}
}

func (d *reservedIPDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m reservedIPDSModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var ip *cloudxino.ReservedIP
	if !m.ID.IsNull() {
		got, err := d.client.ReservedIPs.Get(ctx, m.ID.ValueString())
		if err != nil {
			apiError(&resp.Diagnostics, "read the reserved IP", err)
			return
		}
		ip = got
	} else {
		if m.IPAddress.IsNull() {
			resp.Diagnostics.AddError("Which reserved IP?", "Set ip_address or id.")
			return
		}
		ips, err := d.client.ReservedIPs.List(ctx, &cloudxino.ListReservedIPsOptions{IPAddress: m.IPAddress.ValueString()})
		if err != nil {
			apiError(&resp.Diagnostics, "list reserved IPs", err)
			return
		}
		if len(ips) != 1 {
			resp.Diagnostics.AddError("Reserved IP not found", fmt.Sprintf("The team has no reserved IP %s.", m.IPAddress.ValueString()))
			return
		}
		ip = &ips[0]
	}
	m.ID = types.StringValue(ip.Id)
	m.IPAddress = strValueOrEmpty(ip.IpAddress)
	m.Region = strValueOrEmpty(ip.Region)
	m.ProjectID = strValueOrEmpty(ip.ProjectId)
	m.ServerID = strValue(ip.ServerId)
	m.Status = strValueOrEmpty(ip.Status)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}
