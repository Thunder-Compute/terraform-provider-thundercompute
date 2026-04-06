package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"

	"terraform-provider-thundercompute/internal/client"
)

var (
	_ resource.Resource                = (*SnapshotResource)(nil)
	_ resource.ResourceWithImportState = (*SnapshotResource)(nil)
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
				Required:    true,
				Description: "UUID of the instance to snapshot. Note: this value is not available after import, as the API does not return it in snapshot responses.",
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

	_, err := r.client.CreateSnapshot(ctx, client.CreateSnapshotRequest{
		InstanceID: plan.InstanceID.ValueString(),
		Name:       plan.Name.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating Thunder Compute snapshot",
			fmt.Sprintf("Could not create snapshot %q: %s", plan.Name.ValueString(), err.Error()))
		return
	}

	snap, err := WaitForSnapshot(ctx, r.client, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Error creating Thunder Compute snapshot",
			fmt.Sprintf("Snapshot %q creation timed out or failed: %s", plan.Name.ValueString(), err.Error()))
		return
	}

	plan.ID = types.StringValue(snap.ID)
	plan.Name = types.StringValue(snap.Name)
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
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

