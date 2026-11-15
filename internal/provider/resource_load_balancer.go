package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"

	"github.com/cloudxino/cloudxino-go"
)

const lbTimeout = 15 * time.Minute

type loadBalancerResource struct{ resourceBase }

type loadBalancerModel struct {
	ID              types.String   `tfsdk:"id"`
	Name            types.String   `tfsdk:"name"`
	Region          types.String   `tfsdk:"region"`
	ProjectID       types.String   `tfsdk:"project_id"`
	Algorithm       types.String   `tfsdk:"algorithm"`
	ForwardingRules types.List     `tfsdk:"forwarding_rule"`
	HealthCheck     types.Object   `tfsdk:"health_check"`
	TargetIDs       types.Set      `tfsdk:"target_ids"`
	IPAddress       types.String   `tfsdk:"ip_address"`
	Status          types.String   `tfsdk:"status"`
	Timeouts        timeouts.Value `tfsdk:"timeouts"`
}

var (
	forwardingRuleTypes = map[string]attr.Type{
		"entry_protocol":  types.StringType,
		"entry_port":      types.Int64Type,
		"target_protocol": types.StringType,
		"target_port":     types.Int64Type,
	}
	healthCheckTypes = map[string]attr.Type{
		"protocol": types.StringType,
		"port":     types.Int64Type,
		"path":     types.StringType,
	}
)

func newLoadBalancerResource() resource.Resource { return &loadBalancerResource{} }

var (
	_ resource.ResourceWithConfigure   = (*loadBalancerResource)(nil)
	_ resource.ResourceWithImportState = (*loadBalancerResource)(nil)
)

func (r *loadBalancerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_load_balancer"
}

func (r *loadBalancerResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keepReplace := []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A load balancer: a public IP that spreads traffic over CC2 servers.",
		Attributes: map[string]schema.Attribute{
			"id":     schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":   schema.StringAttribute{Required: true},
			"region": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keepReplace},
			// The API's answer never carries project_id, so it stays what the config says (null = default project).
			"project_id": schema.StringAttribute{Optional: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"algorithm": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("round_robin"),
				Description: "`round_robin` or `least_connections`.",
			},
			"forwarding_rule": schema.ListNestedAttribute{
				Required:    true,
				Description: "tcp or http on `entry_port` to `target_port` on every target (https passes through as tcp).",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"entry_protocol":  schema.StringAttribute{Required: true},
					"entry_port":      schema.Int64Attribute{Required: true},
					"target_protocol": schema.StringAttribute{Optional: true, Computed: true, Description: "Defaults to entry_protocol."},
					"target_port":     schema.Int64Attribute{Required: true},
				}},
			},
			"health_check": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"protocol": schema.StringAttribute{Optional: true, Computed: true, Description: "`tcp` or `http`."},
					"port":     schema.Int64Attribute{Optional: true, Computed: true, Description: "Left out = the first rule's target port."},
					"path":     schema.StringAttribute{Optional: true, Computed: true, Description: "For http checks."},
				},
			},
			"target_ids": schema.SetAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				Default:     setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{})),
				Description: "IDs of the servers that get the traffic.",
			},
			"ip_address": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"status":     schema.StringAttribute{Computed: true},
			"timeouts":   timeouts.Attributes(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func lbRulesIn(v types.List) []cloudxino.ForwardingRule {
	out := []cloudxino.ForwardingRule{}
	for _, el := range v.Elements() {
		a := el.(basetypes.ObjectValue).Attributes()
		out = append(out, cloudxino.ForwardingRule{
			EntryProtocol:  optString(a["entry_protocol"].(types.String)),
			EntryPort:      int(a["entry_port"].(types.Int64).ValueInt64()),
			TargetProtocol: optString(a["target_protocol"].(types.String)),
			TargetPort:     int(a["target_port"].(types.Int64).ValueInt64()),
		})
	}
	return out
}

func lbHealthIn(v types.Object) *cloudxino.HealthCheck {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	a := v.Attributes()
	return &cloudxino.HealthCheck{
		Protocol: optString(a["protocol"].(types.String)),
		Port:     optInt(a["port"].(types.Int64)),
		Path:     optString(a["path"].(types.String)),
	}
}

func (m *loadBalancerModel) fill(lb *cloudxino.LoadBalancer) {
	m.ID = types.StringValue(lb.Id)
	m.Name = strValueOrEmpty(lb.Name)
	m.Region = strValue(lb.Region)
	m.Algorithm = strValue(lb.Algorithm)
	rules := []attr.Value{}
	if lb.ForwardingRules != nil {
		for _, r := range *lb.ForwardingRules {
			target := r.TargetProtocol
			if target == nil {
				target = r.EntryProtocol
			}
			rules = append(rules, types.ObjectValueMust(forwardingRuleTypes, map[string]attr.Value{
				"entry_protocol":  strValue(r.EntryProtocol),
				"entry_port":      intValue(r.EntryPort),
				"target_protocol": strValue(target),
				"target_port":     intValue(r.TargetPort),
			}))
		}
	}
	m.ForwardingRules = types.ListValueMust(types.ObjectType{AttrTypes: forwardingRuleTypes}, rules)
	if hc := lb.HealthCheck; hc != nil {
		m.HealthCheck = types.ObjectValueMust(healthCheckTypes, map[string]attr.Value{
			"protocol": strValue(hc.Protocol),
			"port":     intValue(hc.Port),
			"path":     strValue(hc.Path),
		})
	} else {
		m.HealthCheck = types.ObjectNull(healthCheckTypes)
	}
	m.TargetIDs = setOf(lb.TargetIds)
	m.IPAddress = strValue(lb.IpAddress)
	m.Status = strValue(lb.Status)
}

func (r *loadBalancerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan loadBalancerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, lbTimeout)
	resp.Diagnostics.Append(d...)
	targets := stringsFromSet(ctx, plan.TargetIDs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	body := cloudxino.CreateLoadBalancerRequest{
		Name:            plan.Name.ValueString(),
		Region:          optString(plan.Region),
		ProjectId:       optString(plan.ProjectID),
		Algorithm:       optString(plan.Algorithm),
		ForwardingRules: lbRulesIn(plan.ForwardingRules),
		HealthCheck:     lbHealthIn(plan.HealthCheck),
		TargetIds:       &targets,
	}
	lb, err := r.client.LoadBalancers.Create(ctx, body)
	if err != nil {
		apiError(&resp.Diagnostics, "create the load balancer", err)
		return
	}
	plan.fill(lb)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	ready, err := r.client.LoadBalancers.WaitActive(ctx, lb.Id, &cloudxino.WaitOptions{Interval: waitInterval, Timeout: timeout})
	if ready != nil {
		plan.fill(ready)
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "bring the load balancer up", err)
	}
}

func (r *loadBalancerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state loadBalancerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	lb, err := r.client.LoadBalancers.Get(ctx, state.ID.ValueString())
	if cloudxino.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the load balancer", err)
		return
	}
	state.fill(lb)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *loadBalancerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan loadBalancerModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Update(ctx, lbTimeout)
	resp.Diagnostics.Append(d...)
	targets := stringsFromSet(ctx, plan.TargetIDs, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	rules := lbRulesIn(plan.ForwardingRules)
	body := cloudxino.UpdateLoadBalancerRequest{
		Name:            optString(plan.Name),
		Algorithm:       optString(plan.Algorithm),
		ForwardingRules: &rules,
		HealthCheck:     lbHealthIn(plan.HealthCheck),
		TargetIds:       &targets,
	}
	lb, err := r.client.LoadBalancers.Update(ctx, plan.ID.ValueString(), body)
	if err != nil {
		apiError(&resp.Diagnostics, "update the load balancer", err)
		return
	}
	plan.fill(lb)
	ready, err := r.client.LoadBalancers.WaitActive(ctx, lb.Id, &cloudxino.WaitOptions{Interval: waitInterval, Timeout: timeout})
	if ready != nil {
		plan.fill(ready)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	if err != nil {
		apiError(&resp.Diagnostics, "apply the load balancer change", err)
	}
}

func (r *loadBalancerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state loadBalancerModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, lbTimeout)
	resp.Diagnostics.Append(d...)
	id := state.ID.ValueString()
	if err := r.client.LoadBalancers.Delete(ctx, id); err != nil {
		if !cloudxino.IsNotFound(err) {
			apiError(&resp.Diagnostics, "delete the load balancer", err)
		}
		return
	}
	if err := r.client.LoadBalancers.WaitDeleted(ctx, id, &cloudxino.WaitOptions{Interval: waitInterval, Timeout: timeout}); err != nil {
		apiError(&resp.Diagnostics, "delete the load balancer", err)
	}
}

func (r *loadBalancerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
