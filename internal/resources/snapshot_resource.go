package resources

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
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
	_ resource.Resource                   = (*SnapshotResource)(nil)
	_ resource.ResourceWithImportState    = (*SnapshotResource)(nil)
	_ resource.ResourceWithValidateConfig = (*SnapshotResource)(nil)
)

type SnapshotResource struct {
	client *client.Client
}

type SnapshotResourceModel struct {
	ID                types.String   `tfsdk:"id"`
	InstanceID        types.String   `tfsdk:"instance_id"`
	Name              types.String   `tfsdk:"name"`
	Status            types.String   `tfsdk:"status"`
	CreatedAt         types.Int64    `tfsdk:"created_at"`
	MinimumDiskSizeGB types.Int64    `tfsdk:"minimum_disk_size_gb"`
	Timeouts          timeouts.Value `tfsdk:"timeouts"`
}

func NewSnapshotResource() resource.Resource {
	return &SnapshotResource{}
}

func (r *SnapshotResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_snapshot"
}

func (r *SnapshotResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Thunder Compute instance snapshot.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Snapshot ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"instance_id": schema.StringAttribute{
				Optional:    true,
				Description: "UUID of the instance to snapshot. Required in configuration. Imports should use snapshot_id,instance_uuid because the snapshot list API does not return this value; legacy ID-only imports retain it as null.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Snapshot name (must be unique within the organization).",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Current snapshot status.",
			},
			"created_at": schema.Int64Attribute{
				Computed:    true,
				Description: "Unix timestamp of snapshot creation.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"minimum_disk_size_gb": schema.Int64Attribute{
				Computed:    true,
				Description: "Minimum disk size in GB required to restore this snapshot.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true,
				Delete: true,
			}),
		},
	}
}

func (r *SnapshotResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	if req.Config.Raw.IsNull() {
		return
	}
	var instanceID types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("instance_id"), &instanceID)...)
	if resp.Diagnostics.HasError() || instanceID.IsUnknown() {
		return
	}
	if instanceID.IsNull() || strings.TrimSpace(instanceID.ValueString()) == "" {
		resp.Diagnostics.AddAttributeError(path.Root("instance_id"), "Missing snapshot instance ID", "instance_id is required when declaring a thundercompute_snapshot resource. For imports, use snapshot_id,instance_uuid.")
	}
}

func (r *SnapshotResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Error configuring Thunder Compute snapshot resource",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	r.client = c
}

func (r *SnapshotResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan SnapshotResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, 10*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	existing, err := r.client.GetSnapshotByName(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error checking Thunder Compute snapshot name",
			fmt.Sprintf("Could not verify whether snapshot name %q is already in use: %s", plan.Name.ValueString(), err.Error()))
		return
	}
	if existing != nil {
		resp.Diagnostics.AddAttributeError(path.Root("name"), "Snapshot name is already in use",
			fmt.Sprintf("Snapshot %q already exists with ID %s. Choose a unique name or import that snapshot instead.", plan.Name.ValueString(), existing.ID))
		return
	}

	_, createErr := r.client.CreateSnapshot(ctx, client.CreateSnapshotRequest{
		InstanceID: plan.InstanceID.ValueString(),
		Name:       plan.Name.ValueString(),
	})
	if createErr != nil && !isAmbiguousSnapshotCreateError(createErr) {
		resp.Diagnostics.AddError("Error creating Thunder Compute snapshot",
			fmt.Sprintf("Could not create snapshot %q: %s", plan.Name.ValueString(), createErr.Error()))
		return
	}
	if createErr != nil {
		resp.Diagnostics.AddWarning("Snapshot create response was ambiguous",
			fmt.Sprintf("The create response for snapshot %q was interrupted (%s). Terraform is checking the snapshot list for the server-created identity before deciding whether creation failed.", plan.Name.ValueString(), createErr.Error()))
	}

	snap, err := WaitForSnapshotVisible(ctx, r.client, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating Thunder Compute snapshot",
			fmt.Sprintf("Snapshot %q identity could not be recovered: %s", plan.Name.ValueString(), err.Error()))
		return
	}

	// Persist identity before the potentially long readiness poll. This also
	// retains failed snapshots in state so Terraform can delete them.
	plan.ID = types.StringValue(snap.ID)
	plan.Name = types.StringValue(snap.Name)
	plan.Status = types.StringValue(snap.Status)
	plan.CreatedAt = types.Int64Value(snap.CreatedAt)
	plan.MinimumDiskSizeGB = types.Int64Value(int64(snap.MinimumDiskSizeGB))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if strings.EqualFold(snap.Status, "READY") {
		return
	}
	snap, err = WaitForSnapshot(ctx, r.client, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating Thunder Compute snapshot",
			fmt.Sprintf("Snapshot %q creation timed out or failed: %s", plan.Name.ValueString(), err.Error()))
		return
	}
	plan.Status = types.StringValue(snap.Status)
	plan.CreatedAt = types.Int64Value(snap.CreatedAt)
	plan.MinimumDiskSizeGB = types.Int64Value(int64(snap.MinimumDiskSizeGB))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *SnapshotResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state SnapshotResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	snap, err := r.client.GetSnapshotByID(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error reading Thunder Compute snapshot",
			fmt.Sprintf("Could not read snapshot (ID: %s): %s", state.ID.ValueString(), err.Error()))
		return
	}
	if snap == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	state.Name = types.StringValue(snap.Name)
	state.Status = types.StringValue(snap.Status)
	state.CreatedAt = types.Int64Value(snap.CreatedAt)
	state.MinimumDiskSizeGB = types.Int64Value(int64(snap.MinimumDiskSizeGB))

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *SnapshotResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Error updating Thunder Compute snapshot",
		"Snapshots are immutable. Any change triggers recreation.")
}

func (r *SnapshotResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state SnapshotResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	deleteTimeout, diags := state.Timeouts.Delete(ctx, 5*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, deleteTimeout)
	defer cancel()

	if err := r.client.DeleteSnapshot(ctx, state.ID.ValueString()); err != nil {
		if !client.IsNotFoundError(err) {
			resp.Diagnostics.AddError("Error deleting Thunder Compute snapshot",
				fmt.Sprintf("Could not delete snapshot (ID: %s): %s", state.ID.ValueString(), err.Error()))
		}
	}
}

func (r *SnapshotResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.SplitN(req.ID, ",", 2)
	snapshotID := strings.TrimSpace(parts[0])
	if snapshotID == "" {
		resp.Diagnostics.AddError("Invalid snapshot import ID", "Use snapshot_id,instance_uuid. The snapshot ID cannot be empty.")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), types.StringValue(snapshotID))...)
	if len(parts) == 2 {
		instanceID := strings.TrimSpace(parts[1])
		if instanceID == "" {
			resp.Diagnostics.AddError("Invalid snapshot import ID", "Use snapshot_id,instance_uuid. The instance UUID cannot be empty.")
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("instance_id"), types.StringValue(instanceID))...)
		return
	}
	resp.Diagnostics.AddWarning("Legacy snapshot import ID",
		"The ID-only import format cannot recover instance_id because the API does not return it. The imported state will retain a null instance_id. Prefer snapshot_id,instance_uuid for future imports.")
}

func isAmbiguousSnapshotCreateError(err error) bool {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return true
	}
	return apiErr.StatusCode == 408 || apiErr.StatusCode == 429 || apiErr.StatusCode >= 500
}
