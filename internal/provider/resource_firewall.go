package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/cloudxino/cloudxino-go"
)

type firewallResource struct{ resourceBase }

type firewallModel struct {
	ID            types.String `tfsdk:"id"`
	Name          types.String `tfsdk:"name"`
	InboundRules  types.List   `tfsdk:"inbound_rule"`
	OutboundRules types.List   `tfsdk:"outbound_rule"`
	ServerIDs     types.Set    `tfsdk:"server_ids"`
	Tags          types.Set    `tfsdk:"tags"`
	Status        types.String `tfsdk:"status"`
	ServerCount   types.Int64  `tfsdk:"server_count"`
}

func ruleAttrTypes(addrKey string) map[string]attr.Type {
	return map[string]attr.Type{
		"protocol": types.StringType,
		"ports":    types.StringType,
		addrKey:    types.ListType{ElemType: types.StringType},
	}
}

func newFirewallResource() resource.Resource { return &firewallResource{} }

var (
	_ resource.ResourceWithConfigure   = (*firewallResource)(nil)
	_ resource.ResourceWithImportState = (*firewallResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*firewallResource)(nil)
)

// ModifyPlan keeps status and server_count when nothing the user sets changes. Terraform plans a
// left-out outbound_rule as null and then from state, but by then the framework has already marked
// these "known after apply", which would turn every plan into an update.
func (r *firewallResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return
	}
	var plan, state firewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Name.Equal(state.Name) && plan.InboundRules.Equal(state.InboundRules) &&
		plan.OutboundRules.Equal(state.OutboundRules) && plan.ServerIDs.Equal(state.ServerIDs) &&
		plan.Tags.Equal(state.Tags) {
		plan.Status, plan.ServerCount = state.Status, state.ServerCount
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
	}
}

func (r *firewallResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_firewall"
}

func ruleSchema(addrKey, addrDesc string) schema.NestedAttributeObject {
	return schema.NestedAttributeObject{
		Attributes: map[string]schema.Attribute{
			"protocol": schema.StringAttribute{Required: true, Description: "`tcp`, `udp` or `icmp`."},
			"ports": schema.StringAttribute{
				// Optional only: a computed attribute inside a list is planned as null, never as its
				// state, which would make every plan an update. rulesOut keeps a left-out value null.
				Optional:    true,
				Description: "`22`, `80,443`, `8000-8100` or `all`. Left out (or empty) means all ports; ICMP has no ports.",
			},
			addrKey: schema.ListAttribute{Required: true, ElementType: types.StringType, Description: addrDesc},
		},
	}
}

func (r *firewallResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A Cloud Firewall. It applies to the servers in `server_ids` and to every server with one of its `tags`. " +
			"Traffic a rule does not allow is dropped.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{Required: true},
			"inbound_rule": schema.ListNestedAttribute{
				Optional: true, Computed: true,
				Default:      listdefault.StaticValue(types.ListValueMust(types.ObjectType{AttrTypes: ruleAttrTypes("sources")}, []attr.Value{})),
				Description:  "Traffic allowed in. None = nothing in.",
				NestedObject: ruleSchema("sources", "IPv4 addresses or networks; `0.0.0.0/0` = anywhere."),
			},
			"outbound_rule": schema.ListNestedAttribute{
				Optional: true, Computed: true,
				Description:   "Traffic allowed out. Left out: everything out is allowed.",
				PlanModifiers: []planmodifier.List{listplanmodifier.UseStateForUnknown()},
				NestedObject:  ruleSchema("destinations", "IPv4 addresses or networks; `0.0.0.0/0` = anywhere."),
			},
			"server_ids": schema.SetAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				Default: setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})),
			},
			"tags": schema.SetAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				Default:     setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})),
				Description: "Servers with any of these tags get this firewall.",
			},
			"status":       schema.StringAttribute{Computed: true, Description: "`pending`, `active` or `error`."},
			"server_count": schema.Int64Attribute{Computed: true},
		},
	}
}

// rulesIn reads planned rules for the API.
func rulesIn(ctx context.Context, v types.List, addrKey string, diags *diag.Diagnostics) *[]cloudxino.RuleIn {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	out := []cloudxino.RuleIn{}
	for _, el := range v.Elements() {
		obj := el.(basetypes.ObjectValue)
		attrs := obj.Attributes()
		rule := cloudxino.RuleIn{Protocol: attrs["protocol"].(types.String).ValueString()}
		rule.Ports = optString(attrs["ports"].(types.String))
		addrs := stringsFromList(ctx, attrs[addrKey].(types.List), diags)
		if addrKey == "sources" {
			rule.Sources = &addrs
		} else {
			rule.Destinations = &addrs
		}
		out = append(out, rule)
	}
	return &out
}

// rulesOut builds state from the API's rules, keeping the old text of a rule that means the same.
func rulesOut(api *[]cloudxino.FirewallRule, prior types.List, addrKey string) types.List {
	objType := types.ObjectType{AttrTypes: ruleAttrTypes(addrKey)}
	var old []basetypes.ObjectValue
	if !prior.IsNull() && !prior.IsUnknown() {
		for _, el := range prior.Elements() {
			old = append(old, el.(basetypes.ObjectValue))
		}
	}
	vals := []attr.Value{}
	if api != nil {
		for i, r := range *api {
			proto := strv(r.Protocol)
			ports := strv(r.Ports)
			var addrs []string
			if addrKey == "sources" && r.Sources != nil {
				addrs = *r.Sources
			} else if addrKey == "destinations" && r.Destinations != nil {
				addrs = *r.Destinations
			}
			if i < len(old) && sameRule(old[i], proto, ports, addrs, addrKey) {
				vals = append(vals, old[i])
				continue
			}
			vals = append(vals, types.ObjectValueMust(objType.AttrTypes, map[string]attr.Value{
				"protocol": types.StringValue(proto),
				"ports":    strValueOrNull(ports),
				addrKey:    listOf(&addrs),
			}))
		}
	}
	return types.ListValueMust(objType, vals)
}

func sameRule(old basetypes.ObjectValue, proto, ports string, addrs []string, addrKey string) bool {
	attrs := old.Attributes()
	p, _ := attrs["protocol"].(types.String)
	if p.IsNull() || p.IsUnknown() || !strings.EqualFold(strings.TrimSpace(p.ValueString()), proto) {
		return false
	}
	op, _ := attrs["ports"].(types.String)
	if op.IsUnknown() || canonicalPorts(op.ValueString(), proto) != ports {
		return false
	}
	ol, _ := attrs[addrKey].(types.List)
	if ol.IsNull() || ol.IsUnknown() {
		return false
	}
	var oldAddrs []string
	for _, a := range ol.Elements() {
		oldAddrs = append(oldAddrs, a.(types.String).ValueString())
	}
	return sameStrings(canonicalAddresses(oldAddrs), addrs)
}

func strv(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (m *firewallModel) fill(fw *cloudxino.Firewall) {
	m.ID = types.StringValue(fw.Id)
	m.Name = strValueOrEmpty(fw.Name)
	m.InboundRules = rulesOut(fw.InboundRules, m.InboundRules, "sources")
	m.OutboundRules = rulesOut(fw.OutboundRules, m.OutboundRules, "destinations")
	m.ServerIDs = setOf(fw.ServerIds)
	m.Tags = setOf(fw.Tags)
	m.Status = strValue(fw.Status)
	m.ServerCount = intValue(fw.ServerCount)
}

func (r *firewallResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan firewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := cloudxino.CreateFirewallRequest{
		Name:          plan.Name.ValueString(),
		InboundRules:  rulesIn(ctx, plan.InboundRules, "sources", &resp.Diagnostics),
		OutboundRules: rulesIn(ctx, plan.OutboundRules, "destinations", &resp.Diagnostics),
	}
	ids := stringsFromSet(ctx, plan.ServerIDs, &resp.Diagnostics)
	tags := stringsFromSet(ctx, plan.Tags, &resp.Diagnostics)
	body.ServerIds, body.Tags = &ids, &tags
	if resp.Diagnostics.HasError() {
		return
	}
	fw, err := r.client.Firewalls.Create(ctx, body)
	if err != nil {
		apiError(&resp.Diagnostics, "create the firewall", err)
		return
	}
	plan.fill(fw)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *firewallResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state firewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	fw, err := r.client.Firewalls.Get(ctx, state.ID.ValueString())
	if cloudxino.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the firewall", err)
		return
	}
	state.fill(fw)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *firewallResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan firewallModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ids := stringsFromSet(ctx, plan.ServerIDs, &resp.Diagnostics)
	tags := stringsFromSet(ctx, plan.Tags, &resp.Diagnostics)
	body := cloudxino.UpdateFirewallRequest{
		Name:          optString(plan.Name),
		InboundRules:  rulesIn(ctx, plan.InboundRules, "sources", &resp.Diagnostics),
		OutboundRules: rulesIn(ctx, plan.OutboundRules, "destinations", &resp.Diagnostics),
		ServerIds:     &ids,
		Tags:          &tags,
	}
	if resp.Diagnostics.HasError() {
		return
	}
	fw, err := r.client.Firewalls.Update(ctx, plan.ID.ValueString(), body)
	if err != nil {
		apiError(&resp.Diagnostics, "update the firewall", err)
		return
	}
	plan.fill(fw)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *firewallResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state firewallModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.Firewalls.Delete(ctx, state.ID.ValueString()); err != nil && !cloudxino.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete the firewall", err)
	}
}

func (r *firewallResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func strValueOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}
