package resources

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"regexp"
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
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"terraform-provider-thundercompute/internal/client"
)

var (
	_ resource.Resource                 = (*InstanceResource)(nil)
	_ resource.ResourceWithImportState  = (*InstanceResource)(nil)
	_ resource.ResourceWithModifyPlan   = (*InstanceResource)(nil)
	_ resource.ResourceWithUpgradeState = (*InstanceResource)(nil)
)

type InstanceResource struct {
	client *client.Client
}

type InstanceResourceModel struct {
	// User-configurable
	GPUType             types.String `tfsdk:"gpu_type"`
	Template            types.String `tfsdk:"template"`
	Mode                types.String `tfsdk:"mode"`
	CPUCores            types.Int64  `tfsdk:"cpu_cores"`
	DiskSizeGB          types.Int64  `tfsdk:"disk_size_gb"`
	NumGPUs             types.Int64  `tfsdk:"num_gpus"`
	PublicKey           types.String `tfsdk:"public_key"`
	HTTPPorts           types.Set    `tfsdk:"http_ports"`
	AllowSnapshotModify types.Bool   `tfsdk:"allow_snapshot_modify"`

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
		Version:     1,
		Attributes: map[string]schema.Attribute{
			"gpu_type": schema.StringAttribute{
				Required:    true,
				Description: "GPU type (for example a6000, a100xl, l40, l40s, or h100). API requests are normalized to the public lowercase identifier; a100 is accepted as an alias for a100xl while configured spelling is preserved in state.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"template": schema.StringAttribute{
				Required:    true,
				Description: "OS template or snapshot name. Changing this forces recreation.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"mode": schema.StringAttribute{
				Optional:           true,
				Computed:           true,
				DeprecationMessage: "mode no longer exists in the Thunder Compute API and is retained only for v0.1.0 state compatibility. Remove it from configuration.",
				Description:        "Deprecated compatibility-only field for v0.1.0 state. Instance mode no longer exists and this value is not sent to the API.",
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
					int64validator.OneOf(1, 2, 4, 8),
				},
			},
			"public_key": schema.StringAttribute{
				Optional:    true,
				Description: "SSH public key to inject at creation time.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^$|^(ssh-rsa|ssh-ed25519|ssh-dss|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com)\s+\S{11,}(\s+.*)?$`),
						"must be an OpenSSH public key with a supported key type",
					),
				},
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
					setvalidator.ValueInt64sAre(
						int64validator.Between(1, 65535),
						int64validator.NoneOf(22),
					),
				},
			},
			"allow_snapshot_modify": schema.BoolAttribute{
				Optional:           true,
				Computed:           true,
				Default:            booldefault.StaticBool(false),
				DeprecationMessage: "allow_snapshot_modify is retained for v0.1.0 state compatibility and no longer enables automatic replacement.",
				Description:        "Deprecated compatibility setting. Legacy instances must be snapshotted and recreated manually.",
			},
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Instance UUID (Terraform resource ID).",
				PlanModifiers: []planmodifier.String{
					UnknownStringOnInstanceReplacement(),
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

func (r *InstanceResource) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	schemaType := schemaResp.Schema.Type().TerraformType(ctx)

	return map[int64]resource.StateUpgrader{
		0: {
			StateUpgrader: func(_ context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
				if req.RawState == nil {
					resp.Diagnostics.AddError("Unable to upgrade instance state", "The v0 instance state was empty.")
					return
				}

				rawValue, err := req.RawState.Unmarshal(schemaType)
				if err != nil {
					resp.Diagnostics.AddError("Unable to upgrade instance state", err.Error())
					return
				}
				var values map[string]tftypes.Value
				if err := rawValue.As(&values); err != nil {
					resp.Diagnostics.AddError("Unable to upgrade instance state", err.Error())
					return
				}

				var mode string
				if modeValue, ok := values["mode"]; ok && modeValue.IsKnown() && !modeValue.IsNull() {
					if err := modeValue.As(&mode); err != nil {
						resp.Diagnostics.AddError("Unable to upgrade instance state", err.Error())
						return
					}
				}
				if mode == "" {
					var numGPUsValue big.Float
					if err := values["num_gpus"].As(&numGPUsValue); err != nil {
						resp.Diagnostics.AddError("Unable to upgrade instance state", err.Error())
						return
					}
					numGPUs, _ := numGPUsValue.Int64()
					var ok bool
					mode, ok = legacyModeCompatibilityForGPUCount(numGPUs)
					if !ok {
						resp.Diagnostics.AddError("Unable to upgrade instance state", fmt.Sprintf("num_gpus must be one of 1, 2, 4, or 8; got %d", numGPUs))
						return
					}
				}
				values["mode"] = tftypes.NewValue(tftypes.String, mode)

				dynamicValue, err := tfprotov6.NewDynamicValue(schemaType, tftypes.NewValue(schemaType, values))
				if err != nil {
					resp.Diagnostics.AddError("Unable to upgrade instance state", err.Error())
					return
				}
				resp.DynamicValue = &dynamicValue
			},
		},
	}
}

func legacyModeCompatibilityForGPUCount(numGPUs int64) (string, bool) {
	switch numGPUs {
	case 1, 2:
		return "prototyping", true
	case 4, 8:
		return "production", true
	default:
		return "", false
	}
}

func resolveLegacyModeState(configuredMode string, configuredModeSet bool, stateMode string, planGPUs, stateGPUs int64, hasState bool) (string, string, error) {
	compatibilityMode, ok := legacyModeCompatibilityForGPUCount(planGPUs)
	if !ok {
		return "", "", fmt.Errorf("num_gpus must be one of 1, 2, 4, or 8; got %d", planGPUs)
	}

	if !configuredModeSet {
		if hasState && planGPUs == stateGPUs && stateMode != "" {
			if stateMode != compatibilityMode {
				return stateMode, "The existing Terraform state contains a legacy mode value from v0.1.0. The provider will preserve this compatibility-only value to avoid changing the instance. Remove mode from configuration; recreate the instance manually only if you need to replace it.", nil
			}
			return stateMode, "", nil
		}
		return compatibilityMode, "", nil
	}

	if configuredMode == compatibilityMode {
		if hasState && planGPUs == stateGPUs && stateMode != "" && stateMode != compatibilityMode {
			return "", "", fmt.Errorf("mode %q no longer configures Thunder Compute instances, and this existing resource has a different v0.1.0 compatibility value %q; remove mode from configuration to preserve the instance or recreate it manually", configuredMode, stateMode)
		}
		return configuredMode, "", nil
	}
	if hasState && planGPUs == stateGPUs && configuredMode == stateMode {
		return configuredMode, "Mode no longer configures Thunder Compute instances. Terraform will retain this v0.1.0 compatibility value without replacing or modifying the instance. Remove mode from configuration for future compatibility.", nil
	}

	return "", "", fmt.Errorf("mode %q no longer configures Thunder Compute instances; remove mode from configuration", configuredMode)
}

func (r *InstanceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	resp.Plan = req.Plan
	if req.Plan.Raw.IsNull() {
		return
	}

	var configuredMode, plannedMode, stateMode types.String
	var plannedGPUs, stateGPUs, plannedDisk, stateDisk types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("mode"), &configuredMode)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("mode"), &plannedMode)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("num_gpus"), &plannedGPUs)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("disk_size_gb"), &plannedDisk)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasState := req.State.Raw.IsKnown() && !req.State.Raw.IsNull()
	if hasState {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("mode"), &stateMode)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("num_gpus"), &stateGPUs)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("disk_size_gb"), &stateDisk)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	if hasState && !plannedDisk.IsNull() && !plannedDisk.IsUnknown() && !stateDisk.IsNull() && !stateDisk.IsUnknown() && plannedDisk.ValueInt64() < stateDisk.ValueInt64() {
		resp.Diagnostics.AddAttributeError(
			path.Root("disk_size_gb"),
			"Disk size cannot be decreased",
			fmt.Sprintf("Thunder Compute disks can only grow. disk_size_gb is changing from %d to %d; restore the previous size or recreate the instance manually.", stateDisk.ValueInt64(), plannedDisk.ValueInt64()),
		)
		return
	}

	if plannedGPUs.IsNull() || plannedGPUs.IsUnknown() {
		return
	}
	configuredModeSet := !configuredMode.IsNull() && !configuredMode.IsUnknown()
	mode, warning, err := resolveLegacyModeState(
		configuredMode.ValueString(),
		configuredModeSet,
		stateMode.ValueString(),
		plannedGPUs.ValueInt64(),
		stateGPUs.ValueInt64(),
		hasState,
	)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("mode"), "Obsolete mode configuration", err.Error())
		return
	}
	if warning != "" {
		resp.Diagnostics.AddAttributeWarning(path.Root("mode"), "Preserving legacy instance mode", warning)
	}
	if plannedMode.IsUnknown() || plannedMode.IsNull() || plannedMode.ValueString() != mode {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("mode"), types.StringValue(mode))...)
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
	var configuredPorts types.Set
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("http_ports"), &configuredPorts)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if configuredPorts.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("http_ports"), "Unknown HTTP ports", "http_ports must be known when the instance is created.")
		return
	}
	portsConfigured := !configuredPorts.IsNull()
	desiredPorts := extractInt64Set(configuredPorts)

	createTimeout, diags := plan.Timeouts.Create(ctx, 15*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()

	if err := r.validateInstanceConfiguration(ctx, &plan, nil); err != nil {
		resp.Diagnostics.AddError("Invalid Thunder Compute instance configuration", err.Error())
		return
	}

	createResp, err := r.client.CreateInstance(ctx, client.CreateInstanceRequest{
		CPUCores:   int(plan.CPUCores.ValueInt64()),
		DiskSizeGB: int(plan.DiskSizeGB.ValueInt64()),
		GPUType:    normalizeGPUType(plan.GPUType.ValueString()),
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
	materializePendingInstanceState(&plan)

	// Persist the response identity before any eventually-consistent list call. If
	// a later wait or port reconciliation fails, Terraform still tracks the
	// created instance and can clean it up on the next operation.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	currentIndex, item, err := r.waitForRunning(ctx, createResp.UUID)
	if err != nil {
		resp.Diagnostics.AddError("Error creating Thunder Compute instance",
			fmt.Sprintf("Instance %s did not reach running state: %s", createResp.UUID, err.Error()))
		return
	}
	r.populateInstanceModel(ctx, currentIndex, item, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if portsConfigured {
		addPorts, removePorts := portDiff(intsToInt64s(item.HTTPPorts), desiredPorts)
		if len(addPorts) > 0 || len(removePorts) > 0 {
			portResp, err := r.client.UpdateInstancePorts(ctx, currentIndex, client.PortUpdateRequest{
				AddPorts:    int64sToInts(addPorts),
				RemovePorts: int64sToInts(removePorts),
			})
			if err != nil {
				resp.Diagnostics.AddError("Error configuring Thunder Compute instance ports",
					fmt.Sprintf("Could not expose HTTP ports on instance %s: %s", createResp.UUID, err.Error()))
				return
			}
			item.HTTPPorts = portResp.HTTPPorts
		}
	}

	r.populateInstanceModel(ctx, currentIndex, item, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// materializePendingInstanceState replaces server-computed unknowns with nulls
// until the first list response can populate them. Terraform cannot persist
// unknown values after Create returns, including when a later readiness check
// fails and this partial state is the only record of the created instance.
func materializePendingInstanceState(model *InstanceResourceModel) {
	for _, value := range []*types.String{
		&model.Status,
		&model.IP,
		&model.Name,
		&model.Memory,
		&model.CreatedAt,
	} {
		if value.IsUnknown() {
			*value = types.StringNull()
		}
	}
	if model.Port.IsUnknown() {
		model.Port = types.Int64Null()
	}
	if model.SSHPublicKeys.IsUnknown() {
		model.SSHPublicKeys = types.ListNull(types.StringType)
	}
	if model.HTTPPorts.IsUnknown() {
		model.HTTPPorts = types.SetNull(types.Int64Type)
	}
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
	var configuredPorts types.Set
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("http_ports"), &configuredPorts)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if configuredPorts.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("http_ports"), "Unknown HTTP ports", "http_ports must be known when the instance is updated.")
		return
	}
	portsConfigured := !configuredPorts.IsNull()
	desiredPorts := extractInt64Set(configuredPorts)

	updateTimeout, diags := plan.Timeouts.Update(ctx, 20*time.Minute)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, updateTimeout)
	defer cancel()

	uuid := state.ID.ValueString()
	index, item, err := r.getInstanceByUUIDConfirmingAbsence(ctx, uuid)
	if err != nil {
		resp.Diagnostics.AddError("Error updating Thunder Compute instance",
			fmt.Sprintf("Could not resolve instance %s: %s", uuid, err.Error()))
		return
	}
	if item == nil {
		resp.Diagnostics.AddWarning("Thunder Compute instance disappeared", fmt.Sprintf("Instance %s no longer exists; removing it from Terraform state.", uuid))
		resp.State.RemoveResource(ctx)
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
		if plan.DiskSizeGB.ValueInt64() < state.DiskSizeGB.ValueInt64() {
			resp.Diagnostics.AddAttributeError(path.Root("disk_size_gb"), "Disk size cannot be decreased",
				fmt.Sprintf("Thunder Compute disks can only grow. disk_size_gb is changing from %d to %d.", state.DiskSizeGB.ValueInt64(), plan.DiskSizeGB.ValueInt64()))
			return
		}
		v := int(plan.DiskSizeGB.ValueInt64())
		modReq.DiskSizeGB = &v
		computeChanged = true
	}
	if normalizeGPUType(plan.GPUType.ValueString()) != normalizeGPUType(state.GPUType.ValueString()) {
		v := normalizeGPUType(plan.GPUType.ValueString())
		modReq.GPUType = &v
		computeChanged = true
	}
	if plan.NumGPUs.ValueInt64() != state.NumGPUs.ValueInt64() {
		v := int(plan.NumGPUs.ValueInt64())
		modReq.NumGPUs = &v
		computeChanged = true
	}

	if computeChanged {
		if err := r.validateInstanceConfiguration(ctx, &plan, &state); err != nil {
			resp.Diagnostics.AddError("Invalid Thunder Compute instance configuration", err.Error())
			return
		}
		_, err := r.client.ModifyInstance(ctx, index, modReq)
		if err != nil {
			if requiresManualSnapshotRecreate(err) {
				resp.Diagnostics.AddError("Instance must be recreated manually",
					fmt.Sprintf("Thunder Compute cannot modify instance %s in place (%s). Create a snapshot and recreate the instance manually with the desired configuration. Terraform did not delete or replace the instance; allow_snapshot_modify no longer enables automatic replacement.", uuid, err.Error()))
				return
			}
			resp.Diagnostics.AddError("Error updating Thunder Compute instance",
				fmt.Sprintf("Could not modify instance %s: %s", uuid, err.Error()))
			return
		}
		index, item, err = r.waitForInstanceConfiguration(ctx, uuid, &plan)
		if err != nil {
			resp.Diagnostics.AddError("Error updating Thunder Compute instance",
				fmt.Sprintf("Instance %s did not return to running state after modification: %s", uuid, err.Error()))
			return
		}
	}

	plan.GeneratedKey = state.GeneratedKey
	r.populateInstanceModel(ctx, index, item, &plan, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if portsConfigured {
		addPorts, removePorts := portDiff(intsToInt64s(item.HTTPPorts), desiredPorts)
		if len(addPorts) > 0 || len(removePorts) > 0 {
			portResp, err := r.client.UpdateInstancePorts(ctx, index, client.PortUpdateRequest{
				AddPorts:    int64sToInts(addPorts),
				RemovePorts: int64sToInts(removePorts),
			})
			if err != nil {
				if requiresManualSnapshotRecreate(err) {
					resp.Diagnostics.AddError("Instance ports require manual recreation",
						fmt.Sprintf("Thunder Compute cannot change HTTP ports on legacy instance %s (%s). Snapshot and recreate the instance manually with the desired ports. Terraform did not delete or replace the instance.", uuid, err.Error()))
					return
				}
				resp.Diagnostics.AddError("Error updating Thunder Compute instance ports",
					fmt.Sprintf("Compute changes may have succeeded, but HTTP ports on instance %s could not be reconciled: %s", uuid, err.Error()))
				return
			}
			item.HTTPPorts = portResp.HTTPPorts
		}
	}

	r.populateInstanceModel(ctx, index, item, &plan, &resp.Diagnostics)
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
	index, item, err := r.getInstanceByUUIDConfirmingAbsence(ctx, uuid)
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
	resp.Diagnostics.AddWarning(
		"Instance mode has been removed",
		"The Thunder Compute API no longer has an instance mode. During refresh Terraform will populate a compatibility-only value for v0.1.0 state; do not add mode to configuration.",
	)
}

// isModifyDisabled returns true when Thunder Compute's modify endpoint is temporarily unavailable.
func isModifyDisabled(err error) bool {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 400 && apiErr.ErrorType == "temporarily_disabled"
	}
	return false
}

func requiresManualSnapshotRecreate(err error) bool {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	// The legacy modify API used temporarily_disabled for an explicit contract
	// that instructed callers to snapshot and recreate, not for a retryable 5xx.
	return isModifyDisabled(err) || apiErr.ErrorType == "unsupported_instance_version"
}

func (r *InstanceResource) validateInstanceConfiguration(ctx context.Context, model, previous *InstanceResourceModel) error {
	// The public specs endpoint omits the internal mode. When an existing
	// off-route instance keeps the same GPU count, the modify endpoint preserves
	// its legacy mode, which cannot be represented by /v2/specs. Defer validation
	// to that authoritative endpoint instead of applying the routed mode's spec.
	if previous != nil && model.NumGPUs.ValueInt64() == previous.NumGPUs.ValueInt64() {
		routedMode, ok := legacyModeCompatibilityForGPUCount(previous.NumGPUs.ValueInt64())
		if ok && !previous.Mode.IsNull() && !previous.Mode.IsUnknown() && previous.Mode.ValueString() != "" &&
			!strings.EqualFold(previous.Mode.ValueString(), routedMode) {
			return nil
		}
	}

	specs, err := r.client.GetGPUSpecs(ctx)
	if err != nil {
		return fmt.Errorf("could not load the current GPU specification contract: %w", err)
	}

	configurationKey := fmt.Sprintf("%s_x%d", normalizeGPUType(model.GPUType.ValueString()), model.NumGPUs.ValueInt64())
	var spec *client.GPUSpecConfig
	for key, candidate := range specs {
		if strings.EqualFold(key, configurationKey) {
			candidateCopy := candidate
			spec = &candidateCopy
			break
		}
	}
	if spec == nil {
		return fmt.Errorf("gpu_type %q with num_gpus = %d is not a supported public configuration", model.GPUType.ValueString(), model.NumGPUs.ValueInt64())
	}

	cpuCores := int(model.CPUCores.ValueInt64())
	validCPU := false
	for _, option := range spec.VCPUOptions {
		if cpuCores == option {
			validCPU = true
			break
		}
	}
	if !validCPU {
		return fmt.Errorf("cpu_cores = %d is not valid for %s; choose one of %v", cpuCores, configurationKey, spec.VCPUOptions)
	}

	snapshotMinimum := 0
	if previous == nil {
		templates, err := r.client.GetTemplates(ctx)
		if err != nil {
			return fmt.Errorf("could not determine whether template %q is a snapshot: %w", model.Template.ValueString(), err)
		}
		if _, isTemplate := templates[model.Template.ValueString()]; !isTemplate {
			snapshot, err := r.client.GetSnapshotByName(ctx, model.Template.ValueString())
			if err != nil {
				return fmt.Errorf("could not inspect snapshot %q: %w", model.Template.ValueString(), err)
			}
			if snapshot != nil {
				snapshotMinimum = snapshot.MinimumDiskSizeGB
			}
		}
	}

	diskSize := int(model.DiskSizeGB.ValueInt64())
	diskChanged := previous != nil && model.DiskSizeGB.ValueInt64() != previous.DiskSizeGB.ValueInt64()
	validateStorage := previous == nil ||
		diskChanged ||
		normalizeGPUType(model.GPUType.ValueString()) != normalizeGPUType(previous.GPUType.ValueString()) ||
		model.NumGPUs.ValueInt64() != previous.NumGPUs.ValueInt64()
	if !validateStorage {
		return nil
	}
	if snapshotMinimum > 0 && diskSize < snapshotMinimum {
		return fmt.Errorf("disk_size_gb = %d is smaller than snapshot %q's minimum of %d GB; set disk_size_gb to at least %d to avoid backend-only disk growth", diskSize, model.Template.ValueString(), snapshotMinimum, snapshotMinimum)
	}
	if diskSize < spec.StorageGB.Min {
		return fmt.Errorf("disk_size_gb = %d is below the %s minimum of %d GB", diskSize, configurationKey, spec.StorageGB.Min)
	}
	if diskSize > spec.StorageGB.Max {
		snapshotExpandedCreate := previous == nil && snapshotMinimum > 0 && diskSize <= snapshotMinimum
		// Snapshot provenance only exists in API state. Defer an unchanged disk to
		// the modify endpoint, which permits snapshot-restored disks above max and
		// rejects non-snapshot instances using its authoritative provenance.
		unchangedExistingDisk := previous != nil && !diskChanged
		if !snapshotExpandedCreate && !unchangedExistingDisk {
			return fmt.Errorf("disk_size_gb = %d exceeds the %s maximum of %d GB", diskSize, configurationKey, spec.StorageGB.Max)
		}
	}
	return nil
}

func (r *InstanceResource) waitForInstanceConfiguration(ctx context.Context, uuid string, plan *InstanceResourceModel) (string, *client.InstanceListItem, error) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", nil, fmt.Errorf("waiting for instance %s configuration: %w", uuid, ctx.Err())
		case <-timer.C:
		}

		index, item, err := r.client.GetInstanceByUUID(ctx, uuid)
		if err != nil {
			if client.IsPermanentError(err) {
				return "", nil, fmt.Errorf("permanent error waiting for instance %s configuration: %w", uuid, err)
			}
			timer.Reset(instancePollInterval)
			continue
		}
		if item != nil {
			status := strings.ToUpper(item.Status)
			if status == "STOPPED" || status == "UNKNOWN" {
				return "", nil, fmt.Errorf("instance %s entered terminal status: %s", uuid, item.Status)
			}
			if status == "RUNNING" && item.IP != "" && instanceMatchesComputePlan(item, plan) {
				return index, item, nil
			}
		}
		timer.Reset(instancePollInterval)
	}
}

func instanceMatchesComputePlan(item *client.InstanceListItem, plan *InstanceResourceModel) bool {
	return parseIntOrZero(context.Background(), item.CPUCores) == plan.CPUCores.ValueInt64() &&
		int64(item.Storage) == plan.DiskSizeGB.ValueInt64() &&
		normalizeGPUType(item.GPUType) == normalizeGPUType(plan.GPUType.ValueString()) &&
		parseIntOrZero(context.Background(), item.NumGPUs) == plan.NumGPUs.ValueInt64()
}

// waitForRunning polls until the instance reaches RUNNING with an IP assigned.
// Fails fast on permanent API errors and terminal instance statuses.
var instancePollInterval = 5 * time.Second

func (r *InstanceResource) waitForRunning(ctx context.Context, uuid string) (string, *client.InstanceListItem, error) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", nil, fmt.Errorf("waiting for instance %s: %w", uuid, ctx.Err())
		case <-timer.C:
		}

		index, item, err := r.client.GetInstanceByUUID(ctx, uuid)
		if err != nil {
			if client.IsPermanentError(err) {
				return "", nil, fmt.Errorf("permanent error waiting for instance %s: %w", uuid, err)
			}
			timer.Reset(instancePollInterval)
			continue
		}

		if item != nil {
			status := strings.ToUpper(item.Status)
			switch {
			case status == "RUNNING" && item.IP != "":
				return index, item, nil
			case status == "STOPPED" || status == "UNKNOWN":
				return "", nil, fmt.Errorf("instance %s entered terminal status: %s", uuid, item.Status)
			}
		}

		timer.Reset(instancePollInterval)
	}
}

// readIntoModel fetches the instance by UUID and populates all model fields.
// Sets model.ID to null if the instance no longer exists (signals removal).
func (r *InstanceResource) readIntoModel(ctx context.Context, uuid string, model *InstanceResourceModel, diags *diag.Diagnostics) {
	index, item, err := r.getInstanceByUUIDConfirmingAbsence(ctx, uuid)
	if err != nil {
		diags.AddError("Error reading Thunder Compute instance",
			fmt.Sprintf("Could not read instance %s: %s", uuid, err.Error()))
		return
	}
	if item == nil {
		model.ID = types.StringNull()
		return
	}
	r.populateInstanceModel(ctx, index, item, model, diags)
}

// getInstanceByUUIDConfirmingAbsence repeats the list-backed lookup before
// treating an instance as gone. A single empty list response can be transient.
func (r *InstanceResource) getInstanceByUUIDConfirmingAbsence(ctx context.Context, uuid string) (string, *client.InstanceListItem, error) {
	index, item, err := r.client.GetInstanceByUUID(ctx, uuid)
	if err != nil || item != nil {
		return index, item, err
	}
	return r.client.GetInstanceByUUID(ctx, uuid)
}

func (r *InstanceResource) populateInstanceModel(ctx context.Context, index string, item *client.InstanceListItem, model *InstanceResourceModel, diags *diag.Diagnostics) {
	model.ID = types.StringValue(item.UUID)
	model.Identifier = types.Int64Value(parseIntOrZero(ctx, index))
	model.Status = types.StringValue(item.Status)
	if model.GPUType.IsNull() || model.GPUType.IsUnknown() || model.GPUType.ValueString() == "" {
		model.GPUType = types.StringValue(normalizeGPUType(item.GPUType))
	} else if normalizeGPUType(model.GPUType.ValueString()) != normalizeGPUType(item.GPUType) {
		model.GPUType = types.StringValue(normalizeGPUType(item.GPUType))
	}
	if model.Mode.IsNull() || model.Mode.IsUnknown() || model.Mode.ValueString() == "" {
		mode, ok := legacyModeCompatibilityForGPUCount(parseIntOrZero(ctx, item.NumGPUs))
		if !ok {
			diags.AddError("Unable to populate legacy mode compatibility", fmt.Sprintf("Instance %s returned num_gpus %q, which cannot be represented in v0.1.0 compatibility state.", item.UUID, item.NumGPUs))
			return
		}
		diags.AddWarning("Instance mode has been removed", "The Thunder Compute API no longer has an instance mode. Terraform populated a compatibility-only value for v0.1.0 state; do not add mode to configuration.")
		model.Mode = types.StringValue(mode)
	}
	// Preserve the configured template value because it is replace-only and the
	// list API may report an internal image/snapshot representation.
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

	setValue, d := types.SetValueFrom(ctx, types.Int64Type, intsToInt64s(item.HTTPPorts))
	diags.Append(d...)
	model.HTTPPorts = setValue

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

func intsToInt64s(vals []int) []int64 {
	result := make([]int64, len(vals))
	for i, v := range vals {
		result[i] = int64(v)
	}
	return result
}

func normalizeGPUType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "a100" {
		return "a100xl"
	}
	return value
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
