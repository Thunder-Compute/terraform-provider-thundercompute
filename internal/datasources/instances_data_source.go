package datasources

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"terraform-provider-thundercompute/internal/client"
)

var _ datasource.DataSource = (*InstancesDataSource)(nil)

type InstancesDataSource struct {
	client *client.Client
}

type InstancesDataSourceModel struct {
	Instances []InstanceDataModel `tfsdk:"instances"`
}

type InstanceDataModel struct {
	UUID      types.String `tfsdk:"uuid"`
	Name      types.String `tfsdk:"name"`
	Status    types.String `tfsdk:"status"`
	GPUType   types.String `tfsdk:"gpu_type"`
	Mode      types.String `tfsdk:"mode"`
	Template  types.String `tfsdk:"template"`
	CPUCores  types.Int64  `tfsdk:"cpu_cores"`
	NumGPUs   types.Int64  `tfsdk:"num_gpus"`
	Memory    types.String `tfsdk:"memory"`
	Storage   types.Int64  `tfsdk:"storage"`
	IP        types.String `tfsdk:"ip"`
	Port      types.Int64  `tfsdk:"port"`
	CreatedAt types.String `tfsdk:"created_at"`
}

func NewInstancesDataSource() datasource.DataSource {
	return &InstancesDataSource{}
}

func (d *InstancesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_instances"
}

func (d *InstancesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists all Thunder Compute instances.",
		Attributes: map[string]schema.Attribute{
			"instances": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"uuid":       schema.StringAttribute{Computed: true, Description: "Instance UUID."},
						"name":       schema.StringAttribute{Computed: true, Description: "Instance display name."},
						"status":     schema.StringAttribute{Computed: true, Description: "Current instance status."},
						"gpu_type":   schema.StringAttribute{Computed: true, Description: "GPU type."},
						"mode":       schema.StringAttribute{Computed: true, Description: "Instance mode (prototyping or production)."},
						"template":   schema.StringAttribute{Computed: true, Description: "OS template or snapshot name."},
						"cpu_cores":  schema.Int64Attribute{Computed: true, Description: "Number of vCPU cores."},
						"num_gpus":   schema.Int64Attribute{Computed: true, Description: "Number of GPUs."},
						"memory":     schema.StringAttribute{Computed: true, Description: "Allocated memory."},
						"storage":    schema.Int64Attribute{Computed: true, Description: "Disk size in GB."},
						"ip":         schema.StringAttribute{Computed: true, Description: "Instance IP address."},
						"port":       schema.Int64Attribute{Computed: true, Description: "SSH port."},
						"created_at": schema.StringAttribute{Computed: true, Description: "Creation timestamp."},
					},
				},
			},
		},
	}
}

func (d *InstancesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Error configuring Thunder Compute instances data source",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *InstancesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	instances, err := d.client.ListInstances(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Thunder Compute instances", err.Error())
		return
	}

	// Deterministic ordering by map key (numeric index) for stable plan output
	keys := make([]string, 0, len(instances))
	for k := range instances {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	model := InstancesDataSourceModel{
		Instances: make([]InstanceDataModel, 0, len(instances)),
	}
	for _, k := range keys {
		inst := instances[k]
		model.Instances = append(model.Instances, InstanceDataModel{
			UUID:      types.StringValue(inst.UUID),
			Name:      types.StringValue(inst.Name),
			Status:    types.StringValue(inst.Status),
			GPUType:   types.StringValue(inst.GPUType),
			Mode:      types.StringValue(inst.Mode),
			Template:  types.StringValue(inst.Template),
			CPUCores:  types.Int64Value(parseIntOr(inst.CPUCores, 0)),
			NumGPUs:   types.Int64Value(parseIntOr(inst.NumGPUs, 0)),
			Memory:    types.StringValue(inst.Memory),
			Storage:   types.Int64Value(int64(inst.Storage)),
			IP:        types.StringValue(inst.IP),
			Port:      types.Int64Value(int64(inst.Port)),
			CreatedAt: types.StringValue(inst.CreatedAt),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}

func parseIntOr(s string, fallback int64) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fallback
	}
	return v
}
