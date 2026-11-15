package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

// --- cloudxino_dns_zone --------------------------------------------------------------------------

type dnsZoneResource struct{ resourceBase }

type dnsZoneModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Status      types.String `tfsdk:"status"`
	Nameservers types.List   `tfsdk:"nameservers"`
}

func newDNSZoneResource() resource.Resource { return &dnsZoneResource{} }

var (
	_ resource.ResourceWithConfigure   = (*dnsZoneResource)(nil)
	_ resource.ResourceWithImportState = (*dnsZoneResource)(nil)
)

func (r *dnsZoneResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_zone"
}

func (r *dnsZoneResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A DNS zone hosted on CloudXino's nameservers. Point the domain's nameservers at `nameservers` to make it active.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:      true,
				Description:   "The domain, e.g. `example.com` (lower case, no trailing dot).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{Computed: true, Description: "`pending` until the domain points at our nameservers, then `active`."},
			"nameservers": schema.ListAttribute{
				Computed: true, ElementType: types.StringType,
				Description: "Set these as the domain's nameservers at its registrar.",
			},
		},
	}
}

func (m *dnsZoneModel) fill(z *cloudxino.DnsZone) {
	m.ID = types.StringValue(z.Id)
	if z.Name != nil && !strings.EqualFold(strings.TrimSuffix(m.Name.ValueString(), "."), *z.Name) {
		m.Name = types.StringValue(*z.Name)
	}
	m.Status = strValue(z.Status)
	m.Nameservers = listOf(z.Nameservers)
}

func (r *dnsZoneResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	z, err := r.client.DNS.CreateZone(ctx, plan.Name.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "add the DNS zone", err)
		return
	}
	plan.fill(z)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dnsZoneResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	z, err := r.client.DNS.GetZone(ctx, state.ID.ValueString())
	if cloudxino.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the DNS zone", err)
		return
	}
	state.fill(z)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *dnsZoneResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// every attribute either replaces the zone or is computed
	var plan dnsZoneModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dnsZoneResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsZoneModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DNS.DeleteZone(ctx, state.ID.ValueString()); err != nil && !cloudxino.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete the DNS zone", err)
	}
}

func (r *dnsZoneResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// --- cloudxino_dns_record ------------------------------------------------------------------------

type dnsRecordResource struct{ resourceBase }

type dnsRecordModel struct {
	ID       types.String `tfsdk:"id"`
	ZoneID   types.String `tfsdk:"zone_id"`
	Name     types.String `tfsdk:"name"`
	Type     types.String `tfsdk:"type"`
	Content  types.String `tfsdk:"content"`
	TTL      types.Int64  `tfsdk:"ttl"`
	Priority types.Int64  `tfsdk:"priority"`
	FQDN     types.String `tfsdk:"fqdn"`
}

func newDNSRecordResource() resource.Resource { return &dnsRecordResource{} }

var (
	_ resource.ResourceWithConfigure   = (*dnsRecordResource)(nil)
	_ resource.ResourceWithImportState = (*dnsRecordResource)(nil)
)

func (r *dnsRecordResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_record"
}

func (r *dnsRecordResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A record in a hosted DNS zone. Import with `<zone_id>/<record_id>`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"zone_id": schema.StringAttribute{
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("@"),
				Description: "`@` for the zone itself, or a name relative to it (`www`).",
			},
			"type":     schema.StringAttribute{Required: true, Description: "A, AAAA, CNAME, MX, TXT, NS, SRV, CAA, ..."},
			"content":  schema.StringAttribute{Required: true},
			"ttl":      schema.Int64Attribute{Optional: true, Computed: true, Default: int64default.StaticInt64(3600)},
			"priority": schema.Int64Attribute{Optional: true, Description: "MX and SRV only."},
			"fqdn": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (m *dnsRecordModel) fill(rec *cloudxino.DnsRecord) {
	m.ID = types.StringValue(rec.Id)
	keep := func(old types.String, api *string, fold bool) types.String {
		if api == nil {
			return old
		}
		o := strings.TrimSpace(old.ValueString())
		if !old.IsNull() && !old.IsUnknown() && (o == *api || (fold && strings.EqualFold(strings.TrimSuffix(o, "."), strings.TrimSuffix(*api, ".")))) {
			return old
		}
		return types.StringValue(*api)
	}
	m.Name = keep(m.Name, rec.Name, true)
	m.Type = keep(m.Type, rec.Type, true)
	m.Content = keep(m.Content, rec.Content, false)
	m.TTL = intValue(rec.Ttl)
	m.Priority = intValue(rec.Priority)
	m.FQDN = strValue(rec.Fqdn)
}

func (m *dnsRecordModel) body() cloudxino.RecordIn {
	return cloudxino.RecordIn{
		Name:     optString(m.Name),
		Type:     m.Type.ValueString(),
		Content:  m.Content.ValueString(),
		Ttl:      optInt(m.TTL),
		Priority: optInt(m.Priority),
	}
}

func (r *dnsRecordResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rec, err := r.client.DNS.CreateRecord(ctx, plan.ZoneID.ValueString(), plan.body())
	if err != nil {
		apiError(&resp.Diagnostics, "add the DNS record", err)
		return
	}
	plan.fill(rec)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dnsRecordResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rec, err := r.client.DNS.GetRecord(ctx, state.ZoneID.ValueString(), state.ID.ValueString())
	if cloudxino.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the DNS record", err)
		return
	}
	state.fill(rec)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *dnsRecordResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsRecordModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	rec, err := r.client.DNS.UpdateRecord(ctx, plan.ZoneID.ValueString(), plan.ID.ValueString(), plan.body())
	if err != nil {
		apiError(&resp.Diagnostics, "change the DNS record", err)
		return
	}
	plan.fill(rec)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dnsRecordResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsRecordModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DNS.DeleteRecord(ctx, state.ZoneID.ValueString(), state.ID.ValueString())
	if err != nil && !cloudxino.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete the DNS record", err)
	}
}

func (r *dnsRecordResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	zoneID, recordID, ok := strings.Cut(req.ID, "/")
	if !ok || zoneID == "" || recordID == "" {
		resp.Diagnostics.AddError("Bad import ID", fmt.Sprintf("use <zone_id>/<record_id>, got %q", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("zone_id"), zoneID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), recordID)...)
}
