package provider

import (
	"context"
	"errors"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

const (
	serverCreateTimeout = 20 * time.Minute
	serverUpdateTimeout = 30 * time.Minute
	serverDeleteTimeout = 15 * time.Minute
)

// waitInterval is how often waiters poll; tests make it short.
var waitInterval = 5 * time.Second

type serverResource struct{ resourceBase }

type serverModel struct {
	ID          types.String   `tfsdk:"id"`
	Name        types.String   `tfsdk:"name"`
	Image       types.String   `tfsdk:"image"`
	Size        types.String   `tfsdk:"size"`
	Region      types.String   `tfsdk:"region"`
	IPType      types.String   `tfsdk:"ip_type"`
	LoginMethod types.String   `tfsdk:"login_method"`
	Password    types.String   `tfsdk:"password"`
	VPCID       types.String   `tfsdk:"vpc_id"`
	ProjectID   types.String   `tfsdk:"project_id"`
	SSHKeyIDs   types.Set      `tfsdk:"ssh_key_ids"`
	Tags        types.Set      `tfsdk:"tags"`
	Status      types.String   `tfsdk:"status"`
	IPAddress   types.String   `tfsdk:"ip_address"`
	PrivateIP   types.String   `tfsdk:"private_ip"`
	CPU         types.Int64    `tfsdk:"cpu"`
	Memory      types.Int64    `tfsdk:"memory"`
	Disk        types.Int64    `tfsdk:"disk"`
	CreatedAt   types.String   `tfsdk:"created_at"`
	Timeouts    timeouts.Value `tfsdk:"timeouts"`
}

func newServerResource() resource.Resource { return &serverResource{} }

var (
	_ resource.ResourceWithConfigure   = (*serverResource)(nil)
	_ resource.ResourceWithImportState = (*serverResource)(nil)
)

func (r *serverResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_server"
}

func (r *serverResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keepReplace := []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}
	emptySet := setdefault.StaticValue(types.SetValueMust(types.StringType, []attr.Value{}))
	resp.Schema = schema.Schema{
		Description: "A CC2 cloud server. Created servers are billed from the team's wallet.",
		Attributes: map[string]schema.Attribute{
			"id":    schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"name":  schema.StringAttribute{Required: true, Description: "Hostname: letters, digits and hyphens (1-63)."},
			"image": schema.StringAttribute{Required: true, PlanModifiers: replace, Description: "OS image, e.g. `debian-13` (the console's create page lists them)."},
			"size": schema.StringAttribute{
				Required:    true,
				Description: "Plan slug, e.g. `s-1vcpu-1gb`. Changing it resizes the server in place (disks only grow).",
			},
			"region": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keepReplace, Description: "Region ID; left out = the default region."},
			"ip_type": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("public"), PlanModifiers: replace,
				Description: "`public` (own IPv4) or `nat` (shared IP with a port range).",
				Validators:  []validator.String{oneOf("public", "nat")},
			},
			"login_method": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("key"), PlanModifiers: replace,
				Description: "`key` (SSH keys only) or `password`.",
				Validators:  []validator.String{oneOf("key", "password")},
			},
			"password": schema.StringAttribute{
				Optional: true, Sensitive: true, PlanModifiers: replace,
				Description: "Root password when `login_method` is `password`: 10-128 characters with a lower case letter, an upper case letter and a digit. Kept in state.",
			},
			"vpc_id":     schema.StringAttribute{Optional: true, PlanModifiers: replace},
			"project_id": schema.StringAttribute{Optional: true, Computed: true, PlanModifiers: keepReplace, Description: "Left out = the team's default project."},
			"ssh_key_ids": schema.SetAttribute{
				Optional: true, Computed: true, ElementType: types.StringType, Default: emptySet,
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
				Description:   "IDs of `cloudxino_ssh_key`s to put on the server (at creation only).",
			},
			"tags": schema.SetAttribute{
				Optional: true, Computed: true, ElementType: types.StringType, Default: emptySet,
				Description: "Labels; Cloud Firewalls can apply by tag.",
			},
			"status":     schema.StringAttribute{Computed: true},
			"ip_address": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"private_ip": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"cpu":        schema.Int64Attribute{Computed: true},
			"memory":     schema.Int64Attribute{Computed: true, Description: "MB."},
			"disk":       schema.Int64Attribute{Computed: true, Description: "GB."},
			"created_at": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"timeouts":   timeouts.Attributes(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (m *serverModel) fill(s *cloudxino.Server) {
	m.ID = types.StringValue(s.Id)
	m.Name = strValueOrEmpty(s.Name)
	m.Image = strValueOrEmpty(s.Image)
	m.Size = strValueOrEmpty(s.Size)
	m.Region = strValue(s.Region)
	m.IPType = strValue(s.IpType)
	m.LoginMethod = strValue(s.LoginMethod)
	m.VPCID = strValue(s.VpcId)
	if s.ProjectId != nil {
		m.ProjectID = types.StringValue(*s.ProjectId)
	} else if m.ProjectID.IsUnknown() {
		m.ProjectID = types.StringNull()
	}
	m.SSHKeyIDs = setOf(s.SshKeyIds)
	m.Tags = setOf(s.Tags)
	m.Status = strValue(s.Status)
	m.IPAddress = strValue(s.IpAddress)
	m.PrivateIP = strValue(s.PrivateIp)
	m.CPU = intValue(s.Cpu)
	m.Memory = intValue(s.Memory)
	m.Disk = intValue(s.Disk)
	m.CreatedAt = strValue(s.CreatedAt)
}

func (r *serverResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, serverCreateTimeout)
	resp.Diagnostics.Append(d...)
	keys := stringsFromSet(ctx, plan.SSHKeyIDs, &resp.Diagnostics)
	tags := stringsFromSet(ctx, plan.Tags, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	ipType := cloudxino.CreateMC2RequestIpType(plan.IPType.ValueString())
	login := cloudxino.CreateMC2RequestLoginMethod(plan.LoginMethod.ValueString())
	body := cloudxino.CreateMC2Request{
		Name:        plan.Name.ValueString(),
		Image:       plan.Image.ValueString(),
		Size:        plan.Size.ValueString(),
		Region:      optString(plan.Region),
		IpType:      &ipType,
		LoginMethod: &login,
		Password:    optString(plan.Password),
		VpcId:       optString(plan.VPCID),
		ProjectId:   optString(plan.ProjectID),
		SshKeyIds:   &keys,
		Tags:        &tags,
	}
	srv, err := r.client.Servers.Create(ctx, body)
	if err != nil {
		apiError(&resp.Diagnostics, "create the server", err)
		return
	}
	plan.fill(srv)
	// saved before waiting: if the wait fails, Terraform keeps the server (tainted) instead of losing it
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	ready, err := r.client.Servers.WaitReady(ctx, srv.Id, &cloudxino.WaitOptions{Interval: waitInterval, Timeout: timeout})
	if ready != nil {
		plan.fill(ready)
		resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "bring the server up", err)
	}
}

func (r *serverResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	srv, err := r.client.Servers.Get(ctx, state.ID.ValueString())
	if cloudxino.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the server", err)
		return
	}
	state.fill(srv)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *serverResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state serverModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Update(ctx, serverUpdateTimeout)
	resp.Diagnostics.Append(d...)
	id := state.ID.ValueString()

	if !plan.Name.Equal(state.Name) || !plan.Tags.Equal(state.Tags) {
		tags := stringsFromSet(ctx, plan.Tags, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		srv, err := r.client.Servers.Update(ctx, id, cloudxino.UpdateMC2Request{Name: optString(plan.Name), Tags: &tags})
		if err != nil {
			apiError(&resp.Diagnostics, "update the server", err)
			return
		}
		state.fill(srv)
	}
	if !plan.Size.Equal(state.Size) {
		size := plan.Size.ValueString()
		if _, err := r.client.Servers.Resize(ctx, id, size); err != nil {
			apiError(&resp.Diagnostics, "resize the server", err)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
		srv, err := r.client.Servers.WaitResized(ctx, id, size, &cloudxino.WaitOptions{Interval: waitInterval, Timeout: timeout})
		if srv != nil {
			state.fill(srv)
		}
		if err != nil {
			apiError(&resp.Diagnostics, "resize the server", err)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	srv, err := r.client.Servers.Get(ctx, id)
	if err != nil {
		apiError(&resp.Diagnostics, "read the server", err)
		return
	}
	state.fill(srv)
	state.Password = plan.Password
	state.Timeouts = plan.Timeouts
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *serverResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state serverModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, serverDeleteTimeout)
	resp.Diagnostics.Append(d...)
	id := state.ID.ValueString()
	opts := &cloudxino.WaitOptions{Interval: waitInterval, Timeout: timeout}
	err := r.client.Servers.Delete(ctx, id)
	if cloudxino.IsConflict(err) {
		// still starting, stopping or resizing: wait until it settles, then delete
		if _, werr := r.client.Servers.WaitReady(ctx, id, opts); werr != nil && !isStuck(werr) {
			apiError(&resp.Diagnostics, "delete the server", werr)
			return
		}
		err = r.client.Servers.Delete(ctx, id)
	}
	if cloudxino.IsNotFound(err) {
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "delete the server", err)
		return
	}
	if err := r.client.Servers.WaitDeleted(ctx, id, opts); err != nil {
		apiError(&resp.Diagnostics, "delete the server", err)
	}
}

func isStuck(err error) bool {
	var stuck *cloudxino.ErrStuck
	return errors.As(err, &stuck)
}

func (r *serverResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
