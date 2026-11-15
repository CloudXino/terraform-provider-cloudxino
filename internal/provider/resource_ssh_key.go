package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/cloudxino/cloudxino-go"
)

type sshKeyResource struct{ resourceBase }

type sshKeyModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	Fingerprint types.String `tfsdk:"fingerprint"`
}

func newSSHKeyResource() resource.Resource { return &sshKeyResource{} }

var (
	_ resource.ResourceWithConfigure   = (*sshKeyResource)(nil)
	_ resource.ResourceWithImportState = (*sshKeyResource)(nil)
)

func (r *sshKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *sshKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A public SSH key kept in the team, to put on new servers with `ssh_key_ids`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{Required: true, Description: "1-64 characters."},
			"public_key": schema.StringAttribute{
				Required:      true,
				Description:   "The public key, e.g. `file(\"~/.ssh/id_ed25519.pub\")`. Changing it replaces the key.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"fingerprint": schema.StringAttribute{
				Computed:      true,
				Description:   "SHA256 fingerprint, as `ssh-keygen -lf` prints it.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

// sameKey: the API keeps "<type> <base64>" and drops the comment and surrounding space.
func sameKey(a, b string) bool {
	fa, fb := strings.Fields(a), strings.Fields(b)
	return len(fa) >= 2 && len(fb) >= 2 && fa[0] == fb[0] && fa[1] == fb[1]
}

func (m *sshKeyModel) fill(k *cloudxino.SSHKeyOut) {
	m.ID = types.StringValue(k.Id)
	m.Name = types.StringValue(k.Name)
	m.Fingerprint = types.StringValue(k.Fingerprint)
	if m.PublicKey.IsNull() || m.PublicKey.IsUnknown() || !sameKey(m.PublicKey.ValueString(), k.PublicKey) {
		m.PublicKey = types.StringValue(k.PublicKey)
	}
}

func (r *sshKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.SSHKeys.Create(ctx, plan.Name.ValueString(), plan.PublicKey.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "add the SSH key", err)
		return
	}
	plan.fill(key)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sshKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.SSHKeys.Get(ctx, state.ID.ValueString())
	if cloudxino.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		apiError(&resp.Diagnostics, "read the SSH key", err)
		return
	}
	state.fill(key)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *sshKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sshKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.client.SSHKeys.Rename(ctx, plan.ID.ValueString(), plan.Name.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "rename the SSH key", err)
		return
	}
	plan.fill(key)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sshKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sshKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.SSHKeys.Delete(ctx, state.ID.ValueString()); err != nil && !cloudxino.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete the SSH key", err)
	}
}

func (r *sshKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
