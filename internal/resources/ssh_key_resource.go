package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"terraform-provider-thundercompute/internal/client"
)

var (
	_ resource.Resource                = (*SSHKeyResource)(nil)
	_ resource.ResourceWithImportState = (*SSHKeyResource)(nil)
)

type SSHKeyResource struct {
	client *client.Client
}

type SSHKeyResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	PublicKey   types.String `tfsdk:"public_key"`
	Fingerprint types.String `tfsdk:"fingerprint"`
	KeyType     types.String `tfsdk:"key_type"`
	CreatedAt   types.Int64  `tfsdk:"created_at"`
}

func NewSSHKeyResource() resource.Resource {
	return &SSHKeyResource{}
}

func (r *SSHKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_ssh_key"
}

func (r *SSHKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an SSH key in the Thunder Compute organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "SSH key ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Display name for the SSH key.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"public_key": schema.StringAttribute{
				Required:    true,
				Description: "SSH public key content.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"fingerprint": schema.StringAttribute{
				Computed:    true,
				Description: "SSH key fingerprint.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"key_type": schema.StringAttribute{
				Computed:    true,
				Description: "SSH key type (e.g. ssh-rsa, ssh-ed25519).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_at": schema.Int64Attribute{
				Computed:    true,
				Description: "Unix timestamp of key creation.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *SSHKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Error configuring Thunder Compute SSH key resource",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	r.client = c
}

func (r *SSHKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SSHKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	addResp, err := r.client.AddSSHKey(ctx, client.SSHKeyAddRequest{
		Name:      plan.Name.ValueString(),
		PublicKey: plan.PublicKey.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating Thunder Compute SSH key",
			fmt.Sprintf("Could not create SSH key %q: %s", plan.Name.ValueString(), err.Error()))
		return
	}

	if addResp.Key == nil {
		resp.Diagnostics.AddError("Error creating Thunder Compute SSH key",
			"SSH key was created but the API did not return key details. This may indicate an API change.")
		return
	}

	plan.ID = types.StringValue(addResp.Key.ID)
	plan.Fingerprint = types.StringValue(addResp.Key.Fingerprint)
	plan.KeyType = types.StringValue(addResp.Key.KeyType)
	plan.CreatedAt = types.Int64Value(addResp.Key.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SSHKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SSHKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	key, err := r.client.GetSSHKeyByID(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading Thunder Compute SSH key",
			fmt.Sprintf("Could not read SSH key (ID: %s): %s", state.ID.ValueString(), err.Error()))
		return
	}
	if key == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(key.Name)
	state.PublicKey = types.StringValue(key.PublicKey)
	state.Fingerprint = types.StringValue(key.Fingerprint)
	state.KeyType = types.StringValue(key.KeyType)
	state.CreatedAt = types.Int64Value(key.CreatedAt)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SSHKeyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Error updating Thunder Compute SSH key",
		"SSH keys are immutable. Any change triggers recreation.")
}

func (r *SSHKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SSHKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DeleteSSHKey(ctx, state.ID.ValueString()); err != nil {
		if !client.IsNotFoundError(err) {
			resp.Diagnostics.AddError("Error deleting Thunder Compute SSH key",
				fmt.Sprintf("Could not delete SSH key (ID: %s): %s", state.ID.ValueString(), err.Error()))
		}
	}
}

func (r *SSHKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
