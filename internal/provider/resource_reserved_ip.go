package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

// --- cloudxino_reserved_ip -----------------------------------------------------------------------

type reservedIPResource struct{ resourceBase }

type reservedIPModel struct {
	ID        types.String `tfsdk:"id"`
	IPAddress types.String `tfsdk:"ip_address"`
	Region    types.String `tfsdk:"region"`
	ProjectID types.String `tfsdk:"project_id"`
	ServerID  types.String `tfsdk:"server_id"`
	Status    types.String `tfsdk:"status"`
}

func newReservedIPResource() resource.Resource { return &reservedIPResource{} }

var (
	_ resource.ResourceWithConfigure   = (*reservedIPResource)(nil)
	_ resource.ResourceWithImportState = (*reservedIPResource)(nil)
)

func (r *reservedIPResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_reserved_ip"
}

func (r *reservedIPResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	keepReplace := []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A reserved IP: a public IPv4 address the team keeps until it is destroyed, pointed at a " +
			"CC2 server in the same region. Moving it to another server keeps the address. Billed per hour " +
			"while it exists, assigned or not.\n\n" +
			"Point it at a server either with `server_id` here or with a `cloudxino_reserved_ip_assignment` " +
			"(so the address outlives a server that is replaced), not both.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"ip_address": schema.StringAttribute{
				Computed: true, PlanModifiers: keep, Description: "The address.",
			},
			"region": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: keepReplace,
				Description: "Region ID; left out = the default region. Changing it replaces the reserved IP (a new address).",
			},
			"project_id": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: keepReplace,
				Description: "Left out = the team's default project.",
			},
			"server_id": schema.StringAttribute{
				Optional: true,
				Description: "ID of a `cloudxino_server` in the same region to point the address at. Changing it moves " +
					"the address; removing it unassigns it. Leave it out when a `cloudxino_reserved_ip_assignment` manages the assignment.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "`available` (not pointing anywhere), `assigned` or `deleting`.",
			},
		},
	}
}

// fill copies the API's view. server_id is only tracked when the configuration sets it, so a
// cloudxino_reserved_ip_assignment (or an import) does not make this resource plan an unassign.
func (m *reservedIPModel) fill(ip *cloudxino.ReservedIP) {
	m.ID = types.StringValue(ip.Id)
	m.IPAddress = strValueOrEmpty(ip.IpAddress)
	m.Region = strValueOrEmpty(ip.Region)
	m.ProjectID = strValueOrEmpty(ip.ProjectId)
	m.Status = strValueOrEmpty(ip.Status)
	if !m.ServerID.IsNull() && !m.ServerID.IsUnknown() {
		m.ServerID = strValueOrEmpty(ip.ServerId)
		if ip.ServerId == nil {
			// unassigned outside Terraform: an empty value plans a re-assign
			m.ServerID = types.StringValue("")
		}
	}
}

func (r *reservedIPResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan reservedIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ip, err := r.client.ReservedIPs.Create(ctx, cloudxino.CreateReservedIPRequest{
		Region:    optString(plan.Region),
		ProjectId: optString(plan.ProjectID),
		ServerId:  optString(plan.ServerID),
	})
	if err != nil {
		apiError(&resp.Diagnostics, "reserve an IP", err)
		return
	}
	plan.fill(ip)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *reservedIPResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state reservedIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ip, err := r.client.ReservedIPs.Get(ctx, state.ID.ValueString())
	if cloudxino.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the reserved IP", err)
		return
	}
	state.fill(ip)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *reservedIPResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state reservedIPModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var (
		ip  *cloudxino.ReservedIP
		err error
	)
	id := state.ID.ValueString()
	switch {
	case !plan.ServerID.IsNull() && plan.ServerID.ValueString() != "":
		ip, err = r.client.ReservedIPs.Assign(ctx, id, plan.ServerID.ValueString())
		if err != nil {
			apiError(&resp.Diagnostics, "assign the reserved IP", err)
			return
		}
	case !state.ServerID.IsNull() && state.ServerID.ValueString() != "":
		ip, err = r.client.ReservedIPs.Unassign(ctx, id)
		if err != nil {
			apiError(&resp.Diagnostics, "unassign the reserved IP", err)
			return
		}
	default:
		ip, err = r.client.ReservedIPs.Get(ctx, id)
		if err != nil {
			apiError(&resp.Diagnostics, "read the reserved IP", err)
			return
		}
	}
	plan.fill(ip)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *reservedIPResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state reservedIPModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// the API unassigns first when the address is assigned
	if err := r.client.ReservedIPs.Delete(ctx, state.ID.ValueString()); err != nil && !cloudxino.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete the reserved IP", err)
	}
}

func (r *reservedIPResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// --- cloudxino_reserved_ip_assignment ------------------------------------------------------------

type reservedIPAssignmentResource struct{ resourceBase }

type reservedIPAssignmentModel struct {
	ID           types.String `tfsdk:"id"`
	ReservedIPID types.String `tfsdk:"reserved_ip_id"`
	ServerID     types.String `tfsdk:"server_id"`
	IPAddress    types.String `tfsdk:"ip_address"`
}

func newReservedIPAssignmentResource() resource.Resource { return &reservedIPAssignmentResource{} }

var (
	_ resource.ResourceWithConfigure   = (*reservedIPAssignmentResource)(nil)
	_ resource.ResourceWithImportState = (*reservedIPAssignmentResource)(nil)
)

func (r *reservedIPAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_reserved_ip_assignment"
}

func (r *reservedIPAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Points a `cloudxino_reserved_ip` at a CC2 server. Kept apart from the reserved IP so the " +
			"address stays the team's when the server is replaced: the assignment moves to the new server. " +
			"Destroying it unassigns the address (the reserved IP itself stays).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description: "The reserved IP's ID.",
			},
			"reserved_ip_id": schema.StringAttribute{
				Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description: "ID of the `cloudxino_reserved_ip`.",
			},
			"server_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of a `cloudxino_server` in the reserved IP's region. Changing it moves the address without a gap.",
			},
			"ip_address": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Description: "The reserved address.",
			},
		},
	}
}

func (m *reservedIPAssignmentModel) fill(ip *cloudxino.ReservedIP) {
	m.ID = types.StringValue(ip.Id)
	m.ReservedIPID = types.StringValue(ip.Id)
	m.ServerID = strValueOrEmpty(ip.ServerId)
	m.IPAddress = strValueOrEmpty(ip.IpAddress)
}

func (r *reservedIPAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan reservedIPAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ip, err := r.client.ReservedIPs.Assign(ctx, plan.ReservedIPID.ValueString(), plan.ServerID.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "assign the reserved IP", err)
		return
	}
	plan.fill(ip)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *reservedIPAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state reservedIPAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ReservedIPID.ValueString()
	if id == "" { // just imported
		id = state.ID.ValueString()
	}
	ip, err := r.client.ReservedIPs.Get(ctx, id)
	if cloudxino.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the reserved IP", err)
		return
	}
	if ip.ServerId == nil || *ip.ServerId == "" {
		// unassigned outside Terraform (or its server was deleted): plan a new assignment
		resp.State.RemoveResource(ctx)
		return
	}
	state.fill(ip) // pointing elsewhere: server_id shows the drift and the next apply moves it back
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *reservedIPAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan reservedIPAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ip, err := r.client.ReservedIPs.Assign(ctx, plan.ReservedIPID.ValueString(), plan.ServerID.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "move the reserved IP", err)
		return
	}
	plan.fill(ip)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *reservedIPAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state reservedIPAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ReservedIPID.ValueString()
	ip, err := r.client.ReservedIPs.Get(ctx, id)
	if cloudxino.IsNotFound(err) {
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the reserved IP", err)
		return
	}
	// only undo our own assignment: something else may have moved it since
	if ip.ServerId == nil || *ip.ServerId != state.ServerID.ValueString() {
		return
	}
	if _, err := r.client.ReservedIPs.Unassign(ctx, id); err != nil && !cloudxino.IsNotFound(err) {
		apiError(&resp.Diagnostics, "unassign the reserved IP", err)
	}
}

func (r *reservedIPAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("reserved_ip_id"), req.ID)...)
}
