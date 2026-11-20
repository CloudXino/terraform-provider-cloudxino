package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

const (
	containerHostCreateTimeout = 40 * time.Minute // server + Docker install + the first deployment
	containerHostUpdateTimeout = 40 * time.Minute // a plan change + a deployment
	containerHostDeleteTimeout = 20 * time.Minute

	deploymentNote = "Deployed by Terraform"
)

type containerHostResource struct{ resourceBase }

type containerHostModel struct {
	ID             types.String   `tfsdk:"id"`
	Name           types.String   `tfsdk:"name"`
	Plan           types.String   `tfsdk:"plan"`
	Region         types.String   `tfsdk:"region"`
	ProjectID      types.String   `tfsdk:"project_id"`
	Compose        types.String   `tfsdk:"compose"`
	Env            types.Map      `tfsdk:"env"`
	Domains        types.List     `tfsdk:"domains"`
	IPAddress      types.String   `tfsdk:"ip_address"`
	PortainerURL   types.String   `tfsdk:"portainer_url"`
	Status         types.String   `tfsdk:"status"`
	LastDeployment types.Object   `tfsdk:"last_deployment"`
	CreatedAt      types.String   `tfsdk:"created_at"`
	Timeouts       timeouts.Value `tfsdk:"timeouts"`
}

type containerDomainModel struct {
	Domain  types.String `tfsdk:"domain"`
	Service types.String `tfsdk:"service"`
	Port    types.Int64  `tfsdk:"port"`
}

var containerDomainTypes = map[string]attr.Type{"domain": types.StringType, "service": types.StringType, "port": types.Int64Type}

var deploymentTypes = map[string]attr.Type{"id": types.StringType, "number": types.Int64Type, "status": types.StringType}

func newContainerHostResource() resource.Resource { return &containerHostResource{} }

var (
	_ resource.ResourceWithConfigure   = (*containerHostResource)(nil)
	_ resource.ResourceWithImportState = (*containerHostResource)(nil)
)

func (r *containerHostResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_container_host"
}

func (r *containerHostResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	keepReplace := []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A Managed Container Service (MCS) host: a CC2 server of the team's own with Docker, Docker " +
			"Compose, Traefik (automatic Let's Encrypt certificates) and Portainer, with a `docker-compose.yml` " +
			"deployed to it. Billed per hour from the team's wallet (the server plus the management fee) until destroyed.\n\n" +
			"Creating one takes about 5 minutes; when `compose` is set it is deployed as soon as the host is ready. " +
			"A change to `compose`, `env` or `domains` deploys again and waits for the deployment to finish. " +
			"`compose`, `env` and `domains` are managed only when set: leave one out to manage it elsewhere (for " +
			"example `cxctl containers deploy` from CI).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"name": schema.StringAttribute{
				Required: true, PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Description: "1-63 letters, digits or hyphens, starting and ending with a letter or digit. Changing it replaces the host.",
			},
			"plan": schema.StringAttribute{
				Required: true,
				Description: "Plan slug, e.g. `s-2vcpu-4gb` (the `cloudxino_container_host_plans` data source lists them). " +
					"Changing it resizes the server in place (about a minute of downtime); disks only grow, so a plan with a smaller disk is refused.",
			},
			"region": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: keepReplace,
				Description: "Region ID; left out = the default region. Changing it replaces the host.",
			},
			"project_id": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: keepReplace,
				Description: "Left out = the team's default project. Changing it replaces the host (the API cannot move one).",
			},
			"compose": schema.StringAttribute{
				Optional: true,
				Description: "The `docker-compose.yml` to deploy, e.g. `file(\"docker-compose.yml\")`. Ports 80 and 443 belong to " +
					"Traefik: publish services through `domains` instead. Left out = not managed by Terraform.",
			},
			"env": schema.MapAttribute{
				ElementType: types.StringType, Optional: true, Sensitive: true,
				Description: "Environment variables for every service (also usable as `${NAME}` in the compose file). " +
					"Replaces all of the host's variables; kept encrypted by the API, and in the Terraform state. Left out = not managed by Terraform.",
			},
			"domains": schema.ListNestedAttribute{
				Optional: true,
				Description: "Domains served over HTTPS by Traefik, each routed to a compose service's port. Each domain's A " +
					"record must point at `ip_address` for its certificate to be issued. Replaces all of the host's domains; " +
					"`[]` removes them. Left out = not managed by Terraform.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"domain": schema.StringAttribute{
						Required: true, Validators: []validator.String{canonicalDomain()},
						Description: "Full domain name in lowercase, e.g. `app.example.com`.",
					},
					"service": schema.StringAttribute{Required: true, Description: "A service in the compose file."},
					"port":    schema.Int64Attribute{Required: true, Description: "The port the service listens on inside its container."},
				}},
			},
			"ip_address": schema.StringAttribute{
				Computed: true, PlanModifiers: keep,
				Description: "The host's public IPv4 address: point your domains' A records here.",
			},
			"portainer_url": schema.StringAttribute{
				Computed: true, PlanModifiers: keep,
				Description: "Portainer's address. The first login is in the console (or `cxctl containers credentials`).",
			},
			"status": schema.StringAttribute{
				Computed: true, PlanModifiers: keep,
				Description: "`creating`, `installing`, `ready`, `failed`, `deleting` or `deleted`.",
			},
			"last_deployment": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "The newest deployment, whoever made it; null before the first.",
				Attributes: map[string]schema.Attribute{
					"id":     schema.StringAttribute{Computed: true},
					"number": schema.Int64Attribute{Computed: true, Description: "1, 2, ... per host."},
					"status": schema.StringAttribute{Computed: true, Description: "`running`, `succeeded` or `failed`."},
				},
			},
			"created_at": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"timeouts":   timeouts.Attributes(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func deploymentObject(d *cloudxino.ContainerDeployment) types.Object {
	if d == nil {
		return types.ObjectNull(deploymentTypes)
	}
	return types.ObjectValueMust(deploymentTypes, map[string]attr.Value{
		"id": types.StringValue(d.Id), "number": types.Int64Value(int64(d.Number)), "status": types.StringValue(d.Status),
	})
}

func domainsList(domains []cloudxino.ContainerDomain) types.List {
	vals := []attr.Value{}
	for _, d := range domains {
		vals = append(vals, types.ObjectValueMust(containerDomainTypes, map[string]attr.Value{
			"domain": types.StringValue(d.Domain), "service": types.StringValue(d.Service), "port": types.Int64Value(int64(d.Port)),
		}))
	}
	return types.ListValueMust(types.ObjectType{AttrTypes: containerDomainTypes}, vals)
}

func domainsFrom(ctx context.Context, v types.List, diags *diag.Diagnostics) []cloudxino.ContainerDomain {
	var items []containerDomainModel
	diags.Append(v.ElementsAs(ctx, &items, false)...)
	out := []cloudxino.ContainerDomain{}
	for _, d := range items {
		out = append(out, cloudxino.ContainerDomain{Domain: d.Domain.ValueString(), Service: d.Service.ValueString(), Port: int(d.Port.ValueInt64())})
	}
	return out
}

func envFrom(ctx context.Context, v types.Map, diags *diag.Diagnostics) map[string]string {
	out := map[string]string{}
	diags.Append(v.ElementsAs(ctx, &out, false)...)
	return out
}

func envMap(env map[string]string) types.Map {
	vals := map[string]attr.Value{}
	for k, v := range env {
		vals[k] = types.StringValue(v)
	}
	return types.MapValueMust(types.StringType, vals)
}

// managed: the attribute is set, so Terraform owns it on the host.
func managed(v attr.Value) bool { return !v.IsNull() && !v.IsUnknown() }

func (m *containerHostModel) fill(h *cloudxino.ContainerHost) {
	m.ID = types.StringValue(h.Id)
	m.Name = types.StringValue(h.Name)
	m.Plan = types.StringValue(h.Plan)
	if h.Server.Region != "" {
		m.Region = types.StringValue(h.Server.Region)
	}
	m.ProjectID = types.StringValue(h.ProjectId)
	m.Status = types.StringValue(h.Status)
	m.PortainerURL = types.StringValue(h.PortainerUrl)
	if h.Server.IpAddress != "" {
		m.IPAddress = types.StringValue(h.Server.IpAddress)
	}
	m.LastDeployment = deploymentObject(h.LastDeployment)
	m.CreatedAt = strValue(h.CreatedAt)
	for _, v := range []*types.String{&m.Region, &m.IPAddress} {
		if v.IsUnknown() {
			*v = types.StringNull()
		}
	}
}

// readManaged reads back compose, env and domains, but only the ones Terraform manages.
func (r *containerHostResource) readManaged(ctx context.Context, m *containerHostModel, h *cloudxino.ContainerHost, diags *diag.Diagnostics) {
	if managed(m.Compose) {
		m.Compose = strValue(h.Compose)
	}
	if managed(m.Domains) {
		m.Domains = domainsList(h.Domains)
	}
	if managed(m.Env) {
		env, err := r.client.ContainerHosts.GetEnv(ctx, h.Id)
		if err != nil {
			apiError(diags, "read the host's environment variables", err)
			return
		}
		m.Env = envMap(env)
	}
}

// apply saves env and domains (when managed and changed) without deploying, then makes one
// deployment with all of it and waits for it. It does nothing when nothing managed changed.
// state is nil on create.
func (r *containerHostResource) apply(ctx context.Context, id string, plan, state *containerHostModel, diags *diag.Diagnostics) {
	changed := false
	if managed(plan.Env) && (state == nil || !plan.Env.Equal(state.Env)) {
		env := envFrom(ctx, plan.Env, diags)
		if diags.HasError() {
			return
		}
		if _, err := r.client.ContainerHosts.SetEnv(ctx, id, env, false); err != nil {
			apiError(diags, "save the environment variables", err)
			return
		}
		changed = true
	}
	if managed(plan.Domains) && (state == nil || !plan.Domains.Equal(state.Domains)) {
		domains := domainsFrom(ctx, plan.Domains, diags)
		if diags.HasError() {
			return
		}
		if _, err := r.client.ContainerHosts.SetDomains(ctx, id, domains, false); err != nil {
			apiError(diags, "save the domains", err)
			return
		}
		changed = true
	}
	req := cloudxino.DeployRequest{Note: ptr(deploymentNote)}
	if managed(plan.Compose) {
		if state == nil || !plan.Compose.Equal(state.Compose) {
			changed = true
		}
		req.Compose = optString(plan.Compose)
	}
	if !changed {
		return
	}
	if req.Compose == nil {
		// env or domains changed and the compose file is managed elsewhere: deploy the saved one, if any
		h, err := r.client.ContainerHosts.Get(ctx, id)
		if err != nil {
			apiError(diags, "read the container host", err)
			return
		}
		if h.Compose == nil || *h.Compose == "" {
			return // nothing to deploy yet: the values wait for the first deployment
		}
	}
	dep, err := r.client.ContainerHosts.Deploy(ctx, id, req)
	if err != nil {
		apiError(diags, "deploy", err)
		return
	}
	if _, err := r.client.ContainerHosts.WaitDeployment(ctx, id, dep.Id, &cloudxino.WaitOptions{Interval: waitInterval}); err != nil {
		apiError(diags, fmt.Sprintf("deploy (deployment #%d)", dep.Number), err)
	}
}

func (r *containerHostResource) refresh(ctx context.Context, m *containerHostModel, diags *diag.Diagnostics) {
	h, err := r.client.ContainerHosts.Get(ctx, m.ID.ValueString())
	if err != nil {
		apiError(diags, "read the container host", err)
		return
	}
	m.fill(h)
	r.readManaged(ctx, m, h, diags)
}

func (r *containerHostResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan containerHostModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, containerHostCreateTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	h, err := r.client.ContainerHosts.Create(ctx, cloudxino.CreateContainerHostRequest{
		Name:      plan.Name.ValueString(),
		Plan:      plan.Plan.ValueString(),
		Region:    optString(plan.Region),
		ProjectId: optString(plan.ProjectID),
	})
	if err != nil {
		apiError(&resp.Diagnostics, "create the container host", err)
		return
	}
	// Saved before waiting, without compose / env / domains: if the wait or the first deployment
	// fails, the host is kept (tainted), and after `terraform untaint` the next apply deploys again.
	state := plan
	state.fill(h)
	state.Compose = types.StringNull()
	state.Env = types.MapNull(types.StringType)
	state.Domains = types.ListNull(types.ObjectType{AttrTypes: containerDomainTypes})
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)

	ready, err := r.client.ContainerHosts.WaitReady(ctx, h.Id, &cloudxino.WaitOptions{Interval: waitInterval})
	if ready != nil {
		state.fill(ready)
	}
	if err != nil {
		apiError(&resp.Diagnostics, "bring the container host up", err)
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	r.apply(ctx, h.Id, &plan, nil, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	plan.fill(ready)
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *containerHostResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state containerHostModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	h, err := r.client.ContainerHosts.Get(ctx, state.ID.ValueString())
	if cloudxino.IsNotFound(err) || (err == nil && h.Status == cloudxino.ContainerHostDeleted) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the container host", err)
		return
	}
	state.fill(h)
	r.readManaged(ctx, &state, h, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *containerHostResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state containerHostModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Update(ctx, containerHostUpdateTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	id := state.ID.ValueString()

	if !plan.Plan.Equal(state.Plan) {
		if _, err := r.client.ContainerHosts.ChangePlan(ctx, id, plan.Plan.ValueString()); err != nil {
			apiError(&resp.Diagnostics, "change the plan", err)
			return
		}
		h, err := r.client.ContainerHosts.WaitPlan(ctx, id, plan.Plan.ValueString(), &cloudxino.WaitOptions{Interval: waitInterval})
		if h != nil {
			state.fill(h)
		}
		if err != nil {
			apiError(&resp.Diagnostics, "change the plan", err)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}

	r.apply(ctx, id, &plan, &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		// the old compose / env / domains stay in state, so the next apply deploys again
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	r.refresh(ctx, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *containerHostResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state containerHostModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, containerHostDeleteTimeout)
	resp.Diagnostics.Append(d...)
	id := state.ID.ValueString()
	err := r.client.ContainerHosts.Delete(ctx, id)
	if cloudxino.IsNotFound(err) {
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "delete the container host", err)
		return
	}
	if err := r.client.ContainerHosts.WaitDeleted(ctx, id, &cloudxino.WaitOptions{Interval: waitInterval, Timeout: timeout}); err != nil {
		apiError(&resp.Diagnostics, "delete the container host", err)
	}
}

func (r *containerHostResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
