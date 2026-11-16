package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

const (
	clusterCreateTimeout = 30 * time.Minute
	clusterUpdateTimeout = 30 * time.Minute
	clusterDeleteTimeout = 20 * time.Minute

	// defaultPoolName is the pool the API makes with the cluster (node_size, node_count).
	defaultPoolName = "default"
)

// clusterWaitInterval is how often cluster waiters poll: the API answers one cluster at most
// once per 15s per user. Tests make it short.
var clusterWaitInterval = cloudxino.ClusterPollInterval

type kubernetesClusterResource struct{ resourceBase }

type kubernetesClusterModel struct {
	ID         types.String   `tfsdk:"id"`
	Name       types.String   `tfsdk:"name"`
	Region     types.String   `tfsdk:"region"`
	Version    types.String   `tfsdk:"version"`
	NodeSize   types.String   `tfsdk:"node_size"`
	NodeCount  types.Int64    `tfsdk:"node_count"`
	Ingress    types.Bool     `tfsdk:"ingress"`
	VPCID      types.String   `tfsdk:"vpc_id"`
	ProjectID  types.String   `tfsdk:"project_id"`
	Status     types.String   `tfsdk:"status"`
	Endpoint   types.String   `tfsdk:"endpoint"`
	Kubeconfig types.String   `tfsdk:"kubeconfig"`
	NodePoolID types.String   `tfsdk:"node_pool_id"`
	CreatedAt  types.String   `tfsdk:"created_at"`
	Timeouts   timeouts.Value `tfsdk:"timeouts"`
}

func newKubernetesClusterResource() resource.Resource { return &kubernetesClusterResource{} }

var (
	_ resource.ResourceWithConfigure   = (*kubernetesClusterResource)(nil)
	_ resource.ResourceWithImportState = (*kubernetesClusterResource)(nil)
)

func (r *kubernetesClusterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kubernetes_cluster"
}

func (r *kubernetesClusterResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	keepReplace := []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "A CKS (CloudXino Kubernetes Service) cluster: a managed control plane and one node pool of " +
			"workers on the team's private network. Billed per hour (control plane and nodes) from the team's " +
			"wallet until destroyed; LoadBalancer Services are billed on their own.\n\n" +
			"Creating one usually takes about 5 minutes. `kubeconfig` holds the cluster's admin credentials " +
			"and is kept in the Terraform state: protect the state accordingly.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "1-63 letters, digits or hyphens, starting and ending with a letter or digit. Changing it renames the cluster in place.",
			},
			"region": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: keepReplace,
				Description: "Region ID; left out = the default region. Changing it replaces the cluster.",
			},
			"version": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: keepReplace,
				Description: "Kubernetes version, e.g. `1.36.5`; left out = the newest offered. Changing it replaces the cluster.",
			},
			"node_size": schema.StringAttribute{
				Required: true, PlanModifiers: replace,
				Description: "Worker size slug of the default node pool, e.g. `k8s-2vcpu-4gb` (the console's create page lists them). Changing it replaces the cluster.",
			},
			"node_count": schema.Int64Attribute{
				Optional: true, Computed: true, Default: int64default.StaticInt64(2),
				Description: "Nodes in the default node pool (default 2; the team's per-cluster limit applies). Changing it resizes the pool in place.",
			},
			"ingress": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
				Description:   "Install Traefik as the cluster's one LoadBalancer (default `true`); `false` = bring your own. Changing it replaces the cluster.",
			},
			"vpc_id": schema.StringAttribute{Optional: true, PlanModifiers: replace},
			"project_id": schema.StringAttribute{
				Optional: true, Computed: true, PlanModifiers: keep,
				Description: "Left out = the team's default project. Changing it moves the cluster.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "`pending`, `provisioning`, `active`, `updating`, `deleting` or `error`.",
			},
			"endpoint": schema.StringAttribute{
				Computed: true, PlanModifiers: keep,
				Description: "The Kubernetes API address for kubectl, `https://<host>:<port>`.",
			},
			"kubeconfig": schema.StringAttribute{
				Computed: true, Sensitive: true,
				Description: "Admin kubeconfig (YAML), read while the cluster is active. Kept in state; read again on every refresh and update.",
			},
			"node_pool_id": schema.StringAttribute{
				Computed: true, PlanModifiers: keep,
				Description: "ID of the default node pool.",
			},
			"created_at": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"timeouts":   timeouts.Attributes(ctx, timeouts.Opts{Create: true, Update: true, Delete: true}),
		},
	}
}

func (m *kubernetesClusterModel) fill(cl *cloudxino.Cluster) {
	m.ID = types.StringValue(cl.Id)
	m.Name = types.StringValue(cl.Name)
	m.Region = types.StringValue(cl.Region)
	m.Version = types.StringValue(cl.KubernetesVersion)
	m.ProjectID = types.StringValue(cl.ProjectId)
	m.Status = types.StringValue(cl.Status)
	if cl.ApiEndpoint != nil {
		m.Endpoint = types.StringValue(*cl.ApiEndpoint)
	} else if m.Endpoint.IsUnknown() {
		m.Endpoint = types.StringNull()
	}
	if cl.Ingress != nil {
		m.Ingress = types.BoolValue(*cl.Ingress)
	}
	m.CreatedAt = strValue(cl.CreatedAt)
}

// readExtras fills the default pool and (when active) the kubeconfig. Nothing it cannot read is
// lost: unknown values become null, known ones are kept.
func (r *kubernetesClusterResource) readExtras(ctx context.Context, m *kubernetesClusterModel, diags *diag.Diagnostics) {
	id := m.ID.ValueString()
	pools, err := r.client.Clusters.ListNodePools(ctx, id)
	if err != nil {
		apiError(diags, "read the cluster's node pools", err)
		return
	}
	for _, pool := range pools {
		if pool.Name != defaultPoolName {
			continue
		}
		m.NodePoolID = types.StringValue(pool.Id)
		m.NodeCount = types.Int64Value(int64(pool.Count))
		if pool.SizeSlug != nil && *pool.SizeSlug != "" {
			m.NodeSize = types.StringValue(*pool.SizeSlug)
		}
	}
	if m.Status.ValueString() == cloudxino.ClusterActive {
		kc, err := r.client.Clusters.Kubeconfig(ctx, id)
		if err != nil {
			diags.AddWarning("Cannot read the kubeconfig", err.Error()+" — the next refresh tries again.")
		} else {
			m.Kubeconfig = types.StringValue(kc)
		}
	}
	for _, v := range []*types.String{&m.NodePoolID, &m.Kubeconfig, &m.Endpoint} {
		if v.IsUnknown() {
			*v = types.StringNull()
		}
	}
}

func (r *kubernetesClusterResource) wait(ctx context.Context, m *kubernetesClusterModel, timeout time.Duration, action string, diags *diag.Diagnostics) bool {
	cl, err := r.client.Clusters.WaitActive(ctx, m.ID.ValueString(), &cloudxino.WaitOptions{Interval: clusterWaitInterval, Timeout: timeout})
	if cl != nil {
		m.fill(cl)
	}
	if err != nil {
		apiError(diags, action, err)
		return false
	}
	return true
}

func (r *kubernetesClusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan kubernetesClusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Create(ctx, clusterCreateTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	ingress := plan.Ingress.ValueBool()
	cl, err := r.client.Clusters.Create(ctx, cloudxino.CreateClusterRequest{
		Name:              plan.Name.ValueString(),
		KubernetesVersion: optString(plan.Version),
		Region:            optString(plan.Region),
		NodeSize:          plan.NodeSize.ValueString(),
		NodeCount:         optInt(plan.NodeCount),
		Ingress:           &ingress,
		VpcId:             optString(plan.VPCID),
		ProjectId:         optString(plan.ProjectID),
	})
	if err != nil {
		apiError(&resp.Diagnostics, "create the cluster", err)
		return
	}
	plan.fill(cl)
	for _, v := range []*types.String{&plan.NodePoolID, &plan.Kubeconfig, &plan.Endpoint, &plan.CreatedAt} {
		if v.IsUnknown() {
			*v = types.StringNull()
		}
	}
	// saved before waiting: if the wait fails, Terraform keeps the cluster (tainted) instead of losing it
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	ok := r.wait(ctx, &plan, timeout, "bring the cluster up", &resp.Diagnostics)
	if ok {
		r.readExtras(ctx, &plan, &resp.Diagnostics)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *kubernetesClusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state kubernetesClusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cl, err := r.client.Clusters.Get(ctx, state.ID.ValueString())
	if cloudxino.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the cluster", err)
		return
	}
	state.fill(cl)
	if state.Ingress.IsNull() || state.Ingress.IsUnknown() { // just imported, and the API did not say
		state.Ingress = types.BoolValue(true)
	}
	r.readExtras(ctx, &state, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *kubernetesClusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state kubernetesClusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := plan.Timeouts.Update(ctx, clusterUpdateTimeout)
	resp.Diagnostics.Append(d...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	settings := cloudxino.UpdateClusterRequest{}
	if !plan.Name.Equal(state.Name) {
		settings.Name = optString(plan.Name)
	}
	if !plan.ProjectID.IsUnknown() && !plan.ProjectID.Equal(state.ProjectID) {
		settings.ProjectId = optString(plan.ProjectID)
	}
	if settings.Name != nil || settings.ProjectId != nil {
		cl, err := r.client.Clusters.Update(ctx, id, settings)
		if err != nil {
			apiError(&resp.Diagnostics, "update the cluster", err)
			return
		}
		state.fill(cl)
	}

	if !plan.NodeCount.Equal(state.NodeCount) {
		poolID := state.NodePoolID.ValueString()
		if poolID == "" {
			resp.Diagnostics.AddAttributeError(path.Root("node_count"), "Cannot resize the cluster",
				"The cluster has no node pool named \"default\" to resize.")
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
		count := int(plan.NodeCount.ValueInt64())
		if _, err := r.client.Clusters.UpdateNodePool(ctx, id, poolID, cloudxino.UpdateNodePoolRequest{Count: &count}); err != nil {
			apiError(&resp.Diagnostics, "resize the default node pool", err)
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
		state.NodeCount = plan.NodeCount
		if !r.wait(ctx, &state, timeout, "resize the default node pool", &resp.Diagnostics) {
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}

	cl, err := r.client.Clusters.Get(ctx, id)
	if err != nil {
		apiError(&resp.Diagnostics, "read the cluster", err)
		return
	}
	state.fill(cl)
	r.readExtras(ctx, &state, &resp.Diagnostics)
	state.Timeouts = plan.Timeouts
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *kubernetesClusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state kubernetesClusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	timeout, d := state.Timeouts.Delete(ctx, clusterDeleteTimeout)
	resp.Diagnostics.Append(d...)
	id := state.ID.ValueString()
	err := r.client.Clusters.Delete(ctx, id)
	if cloudxino.IsNotFound(err) {
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "delete the cluster", err)
		return
	}
	if err := r.client.Clusters.WaitDeleted(ctx, id, &cloudxino.WaitOptions{Interval: clusterWaitInterval, Timeout: timeout}); err != nil {
		apiError(&resp.Diagnostics, "delete the cluster", err)
	}
}

func (r *kubernetesClusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
