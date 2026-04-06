package resources

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"terraform-provider-thundercompute/internal/client"
)

var (
	_ resource.Resource                = (*InstanceResource)(nil)
	_ resource.ResourceWithImportState = (*InstanceResource)(nil)
)

type InstanceResource struct {
	client *client.Client
}

type InstanceResourceModel struct {
	// User-configurable
	GPUType              types.String `tfsdk:"gpu_type"`
	Template             types.String `tfsdk:"template"`
	Mode                 types.String `tfsdk:"mode"`
	CPUCores             types.Int64  `tfsdk:"cpu_cores"`
	DiskSizeGB           types.Int64  `tfsdk:"disk_size_gb"`
	NumGPUs              types.Int64  `tfsdk:"num_gpus"`
	PublicKey             types.String `tfsdk:"public_key"`
	HTTPPorts             types.Set    `tfsdk:"http_ports"`
	AllowSnapshotModify  types.Bool   `tfsdk:"allow_snapshot_modify"`

	// Computed
	ID            types.String   `tfsdk:"id"`
	Identifier    types.Int64    `tfsdk:"identifier"`
	GeneratedKey  types.String   `tfsdk:"generated_key"`
	Status        types.String   `tfsdk:"status"`
	IP            types.String   `tfsdk:"ip"`
	Port          types.Int64    `tfsdk:"port"`
	Name          types.String   `tfsdk:"name"`
	Memory        types.String   `tfsdk:"memory"`
	CreatedAt     types.String   `tfsdk:"created_at"`
	SSHPublicKeys types.List     `tfsdk:"ssh_public_keys"`
	Timeouts      timeouts.Value `tfsdk:"timeouts"`
}

func NewInstanceResource() resource.Resource {
	return &InstanceResource{}
}

func (r *InstanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instance"
}

func (r *InstanceResource) Schema(ctx context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Thunder Compute GPU instance.",
		Attributes: map[string]schema.Attribute{
			"gpu_type": schema.StringAttribute{
				Required:    true,
				Description: "GPU type (e.g. A6000, A100, H100). Note: A6000 supports prototyping mode only.",
			},
			"template": schema.StringAttribute{
				Required:    true,
				Description: "OS template or snapshot name. Changing this forces recreation.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"mode": schema.StringAttribute{
				Required:    true,
				Description: "Instance mode: prototyping or production.",
				Validators: []validator.String{
					stringvalidator.OneOf("prototyping", "production"),
				},
			},
			"cpu_cores": schema.Int64Attribute{
				Required:    true,
				Description: "Number of vCPU cores.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"disk_size_gb": schema.Int64Attribute{
				Required:    true,
				Description: "Disk size in GB. Valid range varies by GPU type; see the thundercompute_gpu_specs data source.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"num_gpus": schema.Int64Attribute{
				Required:    true,
				Description: "Number of GPUs.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
			},
			"public_key": schema.StringAttribute{
				Optional:    true,
				Description: "SSH public key to inject at creation time.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"http_ports": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.Int64Type,
				Description: "Set of HTTP ports to expose publicly via thundercompute.net.",
				Validators: []validator.Set{
					setvalidator.ValueInt64sAre(int64validator.Between(1, 65535)),
				},
			},
			"allow_snapshot_modify": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Default:     booldefault.StaticBool(false),
				Description: "When true, permits the snapshot-based modify fallback if the modify API is temporarily unavailable. " +
					"The fallback snapshots the instance, deletes it, and recreates from the snapshot with updated config. " +
					"Note: the recreated instance receives a new generated SSH keypair in addition to keys preserved in the snapshot. " +
					"Port changes may not apply until the modify API is restored.",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Instance UUID (Terraform resource ID).",
				PlanModifiers: []planmodifier.String{
					UnknownStringOnConfigChange(),
				},
			},
			"identifier": schema.Int64Attribute{
				Computed:    true,
				Description: "Internal numeric instance index used by the API. This value may change when instances are created or deleted and should not be relied upon externally.",
				PlanModifiers: []planmodifier.Int64{
					UnknownInt64OnConfigChange(),
				},
			},
			"generated_key": schema.StringAttribute{
				Computed:    true,
				Sensitive:   true,
				Description: "Auto-generated SSH private key (only populated on creation if no public_key was provided).",
				PlanModifiers: []planmodifier.String{
					UnknownStringOnConfigChange(),
				},
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "Current instance status.",
				PlanModifiers: []planmodifier.String{
					UnknownStringOnConfigChange(),
				},
			},
			"ip": schema.StringAttribute{
				Computed:    true,
				Description: "Instance IP address.",
				PlanModifiers: []planmodifier.String{
					UnknownStringOnConfigChange(),
				},
			},
			"port": schema.Int64Attribute{
				Computed:    true,
				Description: "SSH port.",
				PlanModifiers: []planmodifier.Int64{
					UnknownInt64OnConfigChange(),
				},
			},
			"name": schema.StringAttribute{
				Computed:    true,
				Description: "Instance display name.",
				PlanModifiers: []planmodifier.String{
					UnknownStringOnConfigChange(),
				},
			},
			"memory": schema.StringAttribute{
				Computed:    true,
				Description: "Allocated memory.",
				PlanModifiers: []planmodifier.String{
					UnknownStringOnConfigChange(),
				},
			},
			"created_at": schema.StringAttribute{
				Computed:    true,
				Description: "Instance creation timestamp.",
				PlanModifiers: []planmodifier.String{
					UnknownStringOnConfigChange(),
				},
			},
			"ssh_public_keys": schema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "SSH public keys authorized on this instance.",
				PlanModifiers: []planmodifier.List{
					UnknownListOnConfigChange(),
				},
			},
			"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
				Create: true,
				Update: true,
				Delete: true,
			}),
		},
	}
}

func (r *InstanceResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Error configuring Thunder Compute instance resource",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	r.client = c
}

func (r *InstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan InstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createTimeout, diags := plan.Timeouts.Create(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	createResp, err := r.client.CreateInstance(ctx, client.CreateInstanceRequest{
		CPUCores:   int(plan.CPUCores.ValueInt64()),
		DiskSizeGB: int(plan.DiskSizeGB.ValueInt64()),
		GPUType:    plan.GPUType.ValueString(),
		Mode:       plan.Mode.ValueString(),
		NumGPUs:    int(plan.NumGPUs.ValueInt64()),
		Template:   plan.Template.ValueString(),
		PublicKey:  plan.PublicKey.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Error creating Thunder Compute instance", err.Error())
		return
	}

	plan.ID = types.StringValue(createResp.UUID)
	plan.Identifier = types.Int64Value(int64(createResp.Identifier))
	plan.GeneratedKey = types.StringValue(createResp.Key)

	// Persist state immediately so the instance is tracked even if subsequent steps fail.
	// The framework preserves state on Create errors, enabling terraform destroy for cleanup.
	r.readIntoModel(ctx, createResp.UUID, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.waitForRunning(ctx, createResp.UUID); err != nil {
		resp.Diagnostics.AddError("Error creating Thunder Compute instance",
			fmt.Sprintf("Instance %s did not reach running state: %s", createResp.UUID, err.Error()))
		return
	}

	currentIndex, _, err := r.client.GetInstanceByUUID(ctx, createResp.UUID)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Thunder Compute instance",
			fmt.Sprintf("Could not resolve instance %s after provisioning: %s", createResp.UUID, err.Error()))
		return
	}

	if !plan.HTTPPorts.IsNull() && !plan.HTTPPorts.IsUnknown() {
		ports := extractInt64Set(plan.HTTPPorts)
		if len(ports) > 0 {
			if _, err := r.client.ModifyInstance(ctx, currentIndex, client.ModifyInstanceRequest{AddPorts: int64sToInts(ports)}); err != nil {
				resp.Diagnostics.AddError("Error configuring Thunder Compute instance ports",
					fmt.Sprintf("Could not expose HTTP ports on instance %s: %s", createResp.UUID, err.Error()))
				return
			}
		}
	}

	r.readIntoModel(ctx, createResp.UUID, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *InstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state InstanceResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	r.readIntoModel(ctx, state.ID.ValueString(), &state, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if state.ID.IsNull() {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *InstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state InstanceResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateTimeout, diags := plan.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	uuid := state.ID.ValueString()
	index, _, err := r.client.GetInstanceByUUID(ctx, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Error updating Thunder Compute instance",
			fmt.Sprintf("Could not resolve instance %s: %s", uuid, err.Error()))
		return
	}

	modReq := client.ModifyInstanceRequest{}
	computeChanged := false

	if plan.CPUCores.ValueInt64() != state.CPUCores.ValueInt64() {
		v := int(plan.CPUCores.ValueInt64())
		modReq.CPUCores = &v
		computeChanged = true
	}
	if plan.DiskSizeGB.ValueInt64() != state.DiskSizeGB.ValueInt64() {
		v := int(plan.DiskSizeGB.ValueInt64())
		modReq.DiskSizeGB = &v
		computeChanged = true
	}
	if plan.GPUType.ValueString() != state.GPUType.ValueString() {
		v := plan.GPUType.ValueString()
		modReq.GPUType = &v
		computeChanged = true
	}
	if plan.Mode.ValueString() != state.Mode.ValueString() {
		v := plan.Mode.ValueString()
		modReq.Mode = &v
		computeChanged = true
	}
	if plan.NumGPUs.ValueInt64() != state.NumGPUs.ValueInt64() {
		v := int(plan.NumGPUs.ValueInt64())
		modReq.NumGPUs = &v
		computeChanged = true
	}

	oldPorts := extractInt64Set(state.HTTPPorts)
	newPorts := extractInt64Set(plan.HTTPPorts)
	addPorts, removePorts := portDiff(oldPorts, newPorts)
	portsChanged := false
	if len(addPorts) > 0 {
		modReq.AddPorts = int64sToInts(addPorts)
		portsChanged = true
	}
	if len(removePorts) > 0 {
		modReq.RemovePorts = int64sToInts(removePorts)
		portsChanged = true
	}

	changed := computeChanged || portsChanged
	if changed {
		_, err := r.client.ModifyInstance(ctx, index, modReq)
		if err != nil && isModifyDisabled(err) {
			if !computeChanged {
				// Only ports changed -- snapshot fallback would recreate the instance
				// identically and still fail to apply port changes. Just warn.
				resp.Diagnostics.AddWarning("Thunder Compute instance port changes deferred",
					"The modify API is temporarily unavailable. Port changes will be reconciled "+
						"on the next apply when the API is restored.")
				plan.GeneratedKey = state.GeneratedKey
				r.readIntoModel(ctx, uuid, &plan, &resp.Diagnostics)
				if !resp.Diagnostics.HasError() {
					resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
				}
				return
			}
			if !plan.AllowSnapshotModify.ValueBool() {
				resp.Diagnostics.AddError("Error updating Thunder Compute instance",
					"The modify API is temporarily unavailable. Set allow_snapshot_modify = true to permit "+
						"the snapshot-based fallback (snapshot, delete, recreate with new config).")
				return
			}
			oldID := state.ID.ValueString()
			r.updateViaSnapshot(ctx, &plan, &state, addPorts, removePorts, &resp.Diagnostics)
			if resp.Diagnostics.HasError() {
				// Save partial state if a new instance was created before the failure,
				// so Terraform tracks it instead of leaving an orphaned resource.
				if plan.ID.ValueString() != oldID {
					r.readIntoModel(ctx, plan.ID.ValueString(), &plan, &resp.Diagnostics)
					resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
				}
				return
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
			return
		} else if err != nil {
			resp.Diagnostics.AddError("Error updating Thunder Compute instance",
				fmt.Sprintf("Could not modify instance %s: %s", uuid, err.Error()))
			return
		}
	}

	plan.GeneratedKey = state.GeneratedKey

	r.readIntoModel(ctx, uuid, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *InstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state InstanceResourceModel
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

	uuid := state.ID.ValueString()
	index, item, err := r.client.GetInstanceByUUID(ctx, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting Thunder Compute instance",
			fmt.Sprintf("Could not resolve instance %s for deletion: %s", uuid, err.Error()))
		return
	}
	if item == nil {
		return // Already deleted outside Terraform
	}

	if err := r.client.DeleteInstance(ctx, index); err != nil {
		if !client.IsNotFoundError(err) {
			resp.Diagnostics.AddError("Error deleting Thunder Compute instance",
				fmt.Sprintf("Could not delete instance %s: %s", uuid, err.Error()))
		}
	}
}

func (r *InstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// isModifyDisabled returns true when Thunder Compute's modify endpoint is temporarily unavailable.
func isModifyDisabled(err error) bool {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 400 && apiErr.ErrorType == "temporarily_disabled"
	}
	return false
}

// updateViaSnapshot implements the modify fallback: snapshot -> delete -> recreate -> cleanup.
// Port changes are tracked separately: additions are retried on the new instance, removals
// are warned about since the create API does not support port configuration.
func (r *InstanceResource) updateViaSnapshot(ctx context.Context, plan, state *InstanceResourceModel, addPorts, removePorts []int64, diags *diag.Diagnostics) {
	uuid := state.ID.ValueString()
	snapName := "tf-modify-fallback-" + uuid

	// Handle orphaned snapshot from a prior failed fallback attempt.
	existing, _ := r.client.GetSnapshotByName(ctx, snapName)
	if existing != nil {
		upper := strings.ToUpper(existing.Status)
		if upper == "ERROR" || upper == "FAILED" {
			_ = r.client.DeleteSnapshot(ctx, existing.ID)
			existing = nil
		}
	}

	var snap *client.Snapshot
	if existing != nil && strings.ToUpper(existing.Status) != "CREATING" && strings.ToUpper(existing.Status) != "SNAPSHOTTING" {
		// Reuse the ready orphaned snapshot from a prior failed attempt
		snap = existing
	} else {
		if existing == nil {
			if _, err := r.client.CreateSnapshot(ctx, client.CreateSnapshotRequest{
				InstanceID: uuid,
				Name:       snapName,
			}); err != nil {
				diags.AddError("Error updating Thunder Compute instance",
					fmt.Sprintf("Snapshot fallback: could not snapshot instance %s: %s", uuid, err.Error()))
				return
			}
		}

		var err error
		snap, err = WaitForSnapshot(ctx, r.client, snapName)
		if err != nil {
			diags.AddError("Error updating Thunder Compute instance",
				fmt.Sprintf("Snapshot fallback: snapshot %q not ready: %s", snapName, err.Error()))
			return
		}
	}

	index, _, err := r.client.GetInstanceByUUID(ctx, uuid)
	if err != nil {
		diags.AddError("Error updating Thunder Compute instance",
			fmt.Sprintf("Snapshot fallback: could not resolve instance %s for deletion: %s", uuid, err.Error()))
		return
	}
	if err := r.client.DeleteInstance(ctx, index); err != nil {
		diags.AddError("Error updating Thunder Compute instance",
			fmt.Sprintf("Snapshot fallback: could not delete instance %s: %s", uuid, err.Error()))
		return
	}

	createReq := client.CreateInstanceRequest{
		CPUCores:   int(plan.CPUCores.ValueInt64()),
		DiskSizeGB: int(plan.DiskSizeGB.ValueInt64()),
		GPUType:    plan.GPUType.ValueString(),
		Mode:       plan.Mode.ValueString(),
		NumGPUs:    int(plan.NumGPUs.ValueInt64()),
		Template:   snapName,
	}
	// Preserve the user's public key to avoid unnecessary keypair generation
	if !plan.PublicKey.IsNull() && plan.PublicKey.ValueString() != "" {
		createReq.PublicKey = plan.PublicKey.ValueString()
	}
	createResp, err := r.client.CreateInstance(ctx, createReq)
	if err != nil {
		diags.AddError("Error updating Thunder Compute instance",
			fmt.Sprintf("Snapshot fallback: could not recreate instance from snapshot %q: %s", snapName, err.Error()))
		return
	}

	// Update plan immediately so the caller can persist partial state on failure
	plan.ID = types.StringValue(createResp.UUID)
	plan.GeneratedKey = types.StringValue(createResp.Key)

	if err := r.waitForRunning(ctx, createResp.UUID); err != nil {
		diags.AddError("Error updating Thunder Compute instance",
			fmt.Sprintf("Snapshot fallback: new instance %s did not reach running state: %s", createResp.UUID, err.Error()))
		return
	}

	// Port changes via modify -- best-effort since the API may still be disabled
	if len(addPorts) > 0 || len(removePorts) > 0 {
		newIndex, _, err := r.client.GetInstanceByUUID(ctx, createResp.UUID)
		if err == nil && newIndex != "" {
			portReq := client.ModifyInstanceRequest{}
			if len(addPorts) > 0 {
				portReq.AddPorts = int64sToInts(addPorts)
			}
			if len(removePorts) > 0 {
				portReq.RemovePorts = int64sToInts(removePorts)
			}
			if _, err := r.client.ModifyInstance(ctx, newIndex, portReq); err != nil {
				diags.AddWarning("Thunder Compute instance port changes deferred",
					fmt.Sprintf("The modify API is unavailable so HTTP port changes could not be applied to instance %s. "+
						"Ports will be reconciled automatically on the next apply when the API is restored.", createResp.UUID))
			}
		}
	}

	r.readIntoModel(ctx, createResp.UUID, plan, diags)
	if diags.HasError() {
		return
	}

	// Preserve the user's configured template value, not the temporary snapshot name
	plan.Template = state.Template

	if snap != nil {
		if err := r.client.DeleteSnapshot(ctx, snap.ID); err != nil {
			diags.AddWarning("Thunder Compute snapshot cleanup failed",
				fmt.Sprintf("Temporary snapshot %q (ID: %s) was not deleted: %s. Delete it manually.", snapName, snap.ID, err.Error()))
		}
	}
}

// waitForRunning polls until the instance reaches RUNNING with an IP assigned.
// Fails fast on permanent API errors and terminal instance statuses.
func (r *InstanceResource) waitForRunning(ctx context.Context, uuid string) error {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for instance %s: %w", uuid, ctx.Err())
		case <-timer.C:
		}

		_, item, err := r.client.GetInstanceByUUID(ctx, uuid)
		if err != nil {
			if client.IsPermanentError(err) {
				return fmt.Errorf("permanent error waiting for instance %s: %w", uuid, err)
			}
			timer.Reset(5 * time.Second)
			continue
		}

		if item != nil {
			status := strings.ToUpper(item.Status)
			switch {
			case status == "RUNNING" && item.IP != "":
				return nil
			case status == "ERROR" || status == "FAILED" || status == "TERMINATED":
				return fmt.Errorf("instance %s entered terminal status: %s", uuid, item.Status)
			}
		}

		timer.Reset(5 * time.Second)
	}
}

// readIntoModel fetches the instance by UUID and populates all model fields.
// Sets model.ID to null if the instance no longer exists (signals removal).
func (r *InstanceResource) readIntoModel(ctx context.Context, uuid string, model *InstanceResourceModel, diags *diag.Diagnostics) {
	index, item, err := r.client.GetInstanceByUUID(ctx, uuid)
	if err != nil {
		diags.AddError("Error reading Thunder Compute instance",
			fmt.Sprintf("Could not read instance %s: %s", uuid, err.Error()))
		return
	}
	if item == nil {
		model.ID = types.StringNull()
		return
	}

	model.ID = types.StringValue(item.UUID)
	model.Identifier = types.Int64Value(parseIntOrZero(ctx, index))
	model.Status = types.StringValue(item.Status)
	model.GPUType = types.StringValue(item.GPUType)
	model.Mode = types.StringValue(item.Mode)
	// Preserve user's configured template value -- the API may return a snapshot
	// name after the snapshot fallback path, but the user's config is authoritative.
	if model.Template.IsNull() || model.Template.ValueString() == "" {
		model.Template = types.StringValue(item.Template)
	}
	model.IP = types.StringValue(item.IP)
	model.Port = types.Int64Value(int64(item.Port))
	model.Name = types.StringValue(item.Name)
	model.Memory = types.StringValue(item.Memory)
	model.CreatedAt = types.StringValue(item.CreatedAt)

	model.CPUCores = types.Int64Value(parseIntOrZero(ctx, item.CPUCores))
	model.NumGPUs = types.Int64Value(parseIntOrZero(ctx, item.NumGPUs))
	model.DiskSizeGB = types.Int64Value(int64(item.Storage))

	if len(item.HTTPPorts) > 0 {
		portVals := make([]int64, len(item.HTTPPorts))
		for i, p := range item.HTTPPorts {
			portVals[i] = int64(p)
		}
		setValue, d := types.SetValueFrom(ctx, types.Int64Type, portVals)
		diags.Append(d...)
		model.HTTPPorts = setValue
	} else if !model.HTTPPorts.IsNull() {
		emptySet, d := types.SetValueFrom(ctx, types.Int64Type, []int64{})
		diags.Append(d...)
		model.HTTPPorts = emptySet
	} else {
		model.HTTPPorts = types.SetNull(types.Int64Type)
	}

	if len(item.SSHPublicKeys) > 0 {
		listValue, d := types.ListValueFrom(ctx, types.StringType, item.SSHPublicKeys)
		diags.Append(d...)
		model.SSHPublicKeys = listValue
	} else {
		model.SSHPublicKeys = types.ListNull(types.StringType)
	}
}

// --- Helpers ---

func extractInt64Set(s types.Set) []int64 {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	var result []int64
	for _, elem := range s.Elements() {
		if v, ok := elem.(types.Int64); ok {
			result = append(result, v.ValueInt64())
		}
	}
	return result
}

func portDiff(oldPorts, newPorts []int64) (add, remove []int64) {
	oldSet := make(map[int64]bool, len(oldPorts))
	for _, p := range oldPorts {
		oldSet[p] = true
	}
	newSet := make(map[int64]bool, len(newPorts))
	for _, p := range newPorts {
		newSet[p] = true
	}
	for p := range newSet {
		if !oldSet[p] {
			add = append(add, p)
		}
	}
	for p := range oldSet {
		if !newSet[p] {
			remove = append(remove, p)
		}
	}
	return
}

func int64sToInts(vals []int64) []int {
	result := make([]int, len(vals))
	for i, v := range vals {
		result[i] = int(v)
	}
	return result
}

func parseIntOrZero(ctx context.Context, s string) int64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		tflog.Warn(ctx, "failed to parse integer from API response", map[string]interface{}{
			"value": s,
			"error": err.Error(),
		})
		return 0
	}
	return v
}
