package resources

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"terraform-provider-thundercompute/internal/client"
)

var _ resource.Resource = (*InstanceKeyResource)(nil)

type InstanceKeyResource struct {
	client *client.Client
}

type InstanceKeyResourceModel struct {
	ID         types.String `tfsdk:"id"`
	InstanceID types.String `tfsdk:"instance_id"`
	PublicKey  types.String `tfsdk:"public_key"`
}

func NewInstanceKeyResource() resource.Resource {
	return &InstanceKeyResource{}
}

func (r *InstanceKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance_key"
}

func (r *InstanceKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Adds an SSH public key to a running Thunder Compute instance. " +
			"The Thunder Compute API does not support removing keys from instances, so " +
			"destroying this resource removes it from Terraform state but the key remains authorized on the instance.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Resource identifier (instance UUID and key hash).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"instance_id": schema.StringAttribute{
				Required:    true,
				Description: "UUID of the instance to add the SSH key to.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"public_key": schema.StringAttribute{
				Required:    true,
				Description: "SSH public key to authorize on the instance.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *InstanceKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Error configuring Thunder Compute instance key resource",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	r.client = c
}

func (r *InstanceKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan InstanceKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	instanceUUID := plan.InstanceID.ValueString()
	index, item, err := r.client.GetInstanceByUUID(ctx, instanceUUID)
	if err != nil {
		resp.Diagnostics.AddError("Error adding key to Thunder Compute instance",
			fmt.Sprintf("Could not resolve instance %s: %s", instanceUUID, err.Error()))
		return
	}
	if item == nil {
		resp.Diagnostics.AddError("Error adding key to Thunder Compute instance",
			fmt.Sprintf("Instance %s not found.", instanceUUID))
		return
	}

	_, err = r.client.AddKeyToInstance(ctx, index, client.AddKeyToInstanceRequest{
		PublicKey: plan.PublicKey.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error adding key to Thunder Compute instance",
			fmt.Sprintf("Could not add SSH key to instance %s: %s", instanceUUID, err.Error()))
		return
	}

	plan.ID = types.StringValue(instanceKeyID(instanceUUID, plan.PublicKey.ValueString()))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *InstanceKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state InstanceKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	instanceUUID := state.InstanceID.ValueString()
	_, item, err := r.client.GetInstanceByUUID(ctx, instanceUUID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Thunder Compute instance key",
			fmt.Sprintf("Could not read instance %s: %s", instanceUUID, err.Error()))
		return
	}
	if item == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	pubKey := strings.TrimSpace(state.PublicKey.ValueString())
	found := false
	for _, k := range item.SSHPublicKeys {
		if strings.TrimSpace(k) == pubKey {
			found = true
			break
		}
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *InstanceKeyResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Error updating Thunder Compute instance key",
		"Instance keys are immutable. Any change triggers recreation.")
}

func (r *InstanceKeyResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
	// The Thunder Compute API does not support removing SSH keys from instances.
	// Destroying this resource removes it from Terraform state only.
}

// instanceKeyID produces a deterministic composite ID from the instance UUID and public key.
func instanceKeyID(instanceUUID, publicKey string) string {
	h := sha256.Sum256([]byte(publicKey))
	return instanceUUID + ":" + hex.EncodeToString(h[:])[:12]
}
