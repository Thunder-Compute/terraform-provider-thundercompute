package resources

import (
	"context"
	"errors"
	"fmt"
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
	GPUType    types.String `tfsdk:"gpu_type"`
	Template   types.String `tfsdk:"template"`
	CPUCores   types.Int64  `tfsdk:"cpu_cores"`
	DiskSizeGB types.Int64  `tfsdk:"disk_size_gb"`
	NumGPUs    types.Int64  `tfsdk:"num_gpus"`
	PublicKey  types.String `tfsdk:"public_key"`
	HTTPPorts  types.Set    `tfsdk:"http_ports"`

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
				Description: "SSH public key to inject at creation time. Prefer a user-supplied key so Terraform does not need to retain an auto-generated private key in state.",
				Validators: []validator.String{
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^$|^(ssh-rsa|ssh-ed25519|ssh-dss|ecdsa-sha2-nistp(256|384|521)|sk-ssh-ed25519@openssh\.com|sk-ecdsa-sha2-nistp256@openssh\.com)[ \t]+\S{11,}(?:[ \t]+[^\r\n]*)?[\r\n]*$`),
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
				Description: "Set of HTTP ports to expose publicly via thundercompute.net. Omitted or null adopts template, snapshot, or server defaults; an explicit empty set reconciles to no HTTP ports; a non-empty set reconciles exactly.",
				Validators: []validator.Set{
					setvalidator.ValueInt64sAre(
						int64validator.Between(1, 65535),
						int64validator.NoneOf(22),
					),
				},
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
				Description: "Auto-generated SSH private key, populated on creation when no public_key was provided. Sensitive values remain stored in Terraform state; use encrypted remote state with restricted access.",
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

// instanceResourceModelV0 and instanceResourceSchemaV0 describe the serialized
// state written by provider v0.1.0. They are intentionally isolated from the
// current resource model so removed configuration concepts cannot leak back
// into normal planning or lifecycle behavior.
type instanceResourceModelV0 struct {
	GPUType             types.String   `tfsdk:"gpu_type"`
	Template            types.String   `tfsdk:"template"`
	Mode                types.String   `tfsdk:"mode"`
	CPUCores            types.Int64    `tfsdk:"cpu_cores"`
	DiskSizeGB          types.Int64    `tfsdk:"disk_size_gb"`
	NumGPUs             types.Int64    `tfsdk:"num_gpus"`
	PublicKey           types.String   `tfsdk:"public_key"`
	HTTPPorts           types.Set      `tfsdk:"http_ports"`
	AllowSnapshotModify types.Bool     `tfsdk:"allow_snapshot_modify"`
	ID                  types.String   `tfsdk:"id"`
	Identifier          types.Int64    `tfsdk:"identifier"`
	GeneratedKey        types.String   `tfsdk:"generated_key"`
	Status              types.String   `tfsdk:"status"`
	IP                  types.String   `tfsdk:"ip"`
	Port                types.Int64    `tfsdk:"port"`
	Name                types.String   `tfsdk:"name"`
	Memory              types.String   `tfsdk:"memory"`
	CreatedAt           types.String   `tfsdk:"created_at"`
	SSHPublicKeys       types.List     `tfsdk:"ssh_public_keys"`
	Timeouts            timeouts.Value `tfsdk:"timeouts"`
}

func instanceResourceSchemaV0(ctx context.Context) schema.Schema {
	return schema.Schema{Attributes: map[string]schema.Attribute{
		"gpu_type":              schema.StringAttribute{Required: true},
		"template":              schema.StringAttribute{Required: true},
		"mode":                  schema.StringAttribute{Required: true},
		"cpu_cores":             schema.Int64Attribute{Required: true},
		"disk_size_gb":          schema.Int64Attribute{Required: true},
		"num_gpus":              schema.Int64Attribute{Required: true},
		"public_key":            schema.StringAttribute{Optional: true},
		"http_ports":            schema.SetAttribute{Optional: true, Computed: true, ElementType: types.Int64Type},
		"allow_snapshot_modify": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
		"id":                    schema.StringAttribute{Computed: true},
		"identifier":            schema.Int64Attribute{Computed: true},
		"generated_key":         schema.StringAttribute{Computed: true, Sensitive: true},
		"status":                schema.StringAttribute{Computed: true},
		"ip":                    schema.StringAttribute{Computed: true},
		"port":                  schema.Int64Attribute{Computed: true},
		"name":                  schema.StringAttribute{Computed: true},
		"memory":                schema.StringAttribute{Computed: true},
		"created_at":            schema.StringAttribute{Computed: true},
		"ssh_public_keys":       schema.ListAttribute{Computed: true, ElementType: types.StringType},
		"timeouts": timeouts.Attributes(ctx, timeouts.Opts{
			Create: true,
			Update: true,
			Delete: true,
		}),
	}}
}

func (r *InstanceResource) UpgradeState(ctx context.Context) map[int64]resource.StateUpgrader {
	priorSchema := instanceResourceSchemaV0(ctx)
	return map[int64]resource.StateUpgrader{
		0: {
			PriorSchema: &priorSchema,
			StateUpgrader: func(ctx context.Context, req resource.UpgradeStateRequest, resp *resource.UpgradeStateResponse) {
				if req.State == nil {
					resp.Diagnostics.AddError("Unable to upgrade instance state", "The v0 instance state was empty.")
					return
				}

				var prior instanceResourceModelV0
				resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
				if resp.Diagnostics.HasError() {
					return
				}

				upgraded := InstanceResourceModel{
					GPUType:       prior.GPUType,
					Template:      prior.Template,
					CPUCores:      prior.CPUCores,
					DiskSizeGB:    prior.DiskSizeGB,
					NumGPUs:       prior.NumGPUs,
					PublicKey:     prior.PublicKey,
					HTTPPorts:     prior.HTTPPorts,
					ID:            prior.ID,
					Identifier:    prior.Identifier,
					GeneratedKey:  prior.GeneratedKey,
					Status:        prior.Status,
					IP:            prior.IP,
					Port:          prior.Port,
					Name:          prior.Name,
					Memory:        prior.Memory,
					CreatedAt:     prior.CreatedAt,
					SSHPublicKeys: prior.SSHPublicKeys,
					Timeouts:      prior.Timeouts,
				}
				resp.Diagnostics.Append(resp.State.Set(ctx, &upgraded)...)
			},
		},
	}
}

func (r *InstanceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	resp.Plan = req.Plan
	if req.Plan.Raw.IsNull() {
		return
	}

	var plannedDisk, stateDisk types.Int64
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("disk_size_gb"), &plannedDisk)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasState := req.State.Raw.IsKnown() && !req.State.Raw.IsNull()
	if hasState {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("disk_size_gb"), &stateDisk)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	replacementPlanned := hasState && anyAttributesChanged(ctx, req.Plan, req.State, instanceReplacementTriggerAttributes)
	if hasState && !replacementPlanned && !plannedDisk.IsNull() && !plannedDisk.IsUnknown() && !stateDisk.IsNull() && !stateDisk.IsUnknown() && plannedDisk.ValueInt64() < stateDisk.ValueInt64() {
		resp.Diagnostics.AddAttributeError(
			path.Root("disk_size_gb"),
			"Disk size cannot be decreased",
			fmt.Sprintf("Thunder Compute disks can only grow. disk_size_gb is changing from %d to %d; restore the previous size or recreate the instance manually.", stateDisk.ValueInt64(), plannedDisk.ValueInt64()),
		)
		return
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
	if setContainsUnknown(configuredPorts) {
		resp.Diagnostics.AddAttributeError(path.Root("http_ports"), "Unknown HTTP ports", "http_ports and every port in it must be known when the instance is created.")
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
		PublicKey:  strings.TrimSpace(plan.PublicKey.ValueString()),
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
	if setContainsUnknown(configuredPorts) {
		resp.Diagnostics.AddAttributeError(path.Root("http_ports"), "Unknown HTTP ports", "http_ports and every port in it must be known when the instance is updated.")
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
					fmt.Sprintf("Thunder Compute cannot modify instance %s in place (%s). Create a snapshot and recreate the instance manually with the desired configuration. Terraform did not delete or replace the instance.", uuid, err.Error()))
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
						fmt.Sprintf("Thunder Compute cannot change HTTP ports on instance %s (%s). Snapshot and recreate the instance manually with the desired ports. Terraform did not delete or replace the instance.", uuid, err.Error()))
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
}

func requiresManualSnapshotRecreate(err error) bool {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.ErrorType == "unsupported_instance_version"
}

func (r *InstanceResource) validateInstanceConfiguration(ctx context.Context, model, previous *InstanceResourceModel) error {
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
			if status == "STOPPED" {
				return "", nil, fmt.Errorf("instance %s entered terminal status: %s", uuid, item.Status)
			}
			if status == "RUNNING" && item.IP != "" && instanceMatchesComputePlan(ctx, item, plan) {
				return index, item, nil
			}
		}
		timer.Reset(instancePollInterval)
	}
}

func instanceMatchesComputePlan(ctx context.Context, item *client.InstanceListItem, plan *InstanceResourceModel) bool {
	return parseIntOrZero(ctx, item.CPUCores) == plan.CPUCores.ValueInt64() &&
		int64(item.Storage) == plan.DiskSizeGB.ValueInt64() &&
		normalizeGPUType(item.GPUType) == normalizeGPUType(plan.GPUType.ValueString()) &&
		parseIntOrZero(ctx, item.NumGPUs) == plan.NumGPUs.ValueInt64()
}

// waitForRunning polls until the instance reaches RUNNING with an IP assigned.
// Fails fast on permanent API errors and terminal instance statuses. UNKNOWN
// is not terminal: the API reports it whenever the control plane cannot
// determine state yet (including the normal window before a new instance is
// visible in the cluster cache), so it polls until the operation timeout.
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
			case status == "STOPPED":
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

	// Separate the confirmation lookup from the initial miss. Back-to-back reads
	// can observe the same stale list response and incorrectly remove a live
	// instance from state.
	timer := time.NewTimer(instancePollInterval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", nil, ctx.Err()
	case <-timer.C:
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

	if len(item.HTTPPorts) == 0 && (model.HTTPPorts.IsNull() || model.HTTPPorts.IsUnknown()) {
		// Preserve the distinction between omitted ports and an explicit empty set.
		// Both produce no API ports, but only the omitted form should remain null.
		model.HTTPPorts = types.SetNull(types.Int64Type)
	} else {
		setValue, d := types.SetValueFrom(ctx, types.Int64Type, intsToInt64s(item.HTTPPorts))
		diags.Append(d...)
		model.HTTPPorts = setValue
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

// setContainsUnknown reports whether the set itself or any of its elements is
// unknown. A set referencing another resource's computed value stays known at
// the collection level while holding unknown elements, which extractInt64Set
// would otherwise read as zero values.
func setContainsUnknown(s types.Set) bool {
	if s.IsUnknown() {
		return true
	}
	for _, elem := range s.Elements() {
		if elem.IsUnknown() {
			return true
		}
	}
	return false
}

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
