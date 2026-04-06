package datasources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"terraform-provider-thundercompute/internal/client"
)

var _ datasource.DataSource = (*GPUSpecsDataSource)(nil)

type GPUSpecsDataSource struct {
	client *client.Client
}

type GPUSpecsDataSourceModel struct {
	Specs map[string]GPUSpecModel `tfsdk:"specs"`
}

type GPUSpecModel struct {
	DisplayName   types.String `tfsdk:"display_name"`
	GPUCount      types.Int64  `tfsdk:"gpu_count"`
	Mode          types.String `tfsdk:"mode"`
	RAMPerVCPUGiB types.Int64  `tfsdk:"ram_per_vcpu_gib"`
	VRAMGB        types.Int64  `tfsdk:"vram_gb"`
	MaxCPUPerGPU  types.Int64  `tfsdk:"max_cpu_per_gpu"`
	StorageMinGB  types.Int64  `tfsdk:"storage_min_gb"`
	StorageMaxGB  types.Int64  `tfsdk:"storage_max_gb"`
}

func NewGPUSpecsDataSource() datasource.DataSource {
	return &GPUSpecsDataSource{}
}

func (d *GPUSpecsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gpu_specs"
}

func (d *GPUSpecsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Retrieves GPU specifications for all available Thunder Compute GPU types.",
		Attributes: map[string]schema.Attribute{
			"specs": schema.MapNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"display_name":      schema.StringAttribute{Computed: true, Description: "Human-readable GPU name."},
						"gpu_count":         schema.Int64Attribute{Computed: true, Description: "Number of GPUs in this configuration."},
						"mode":              schema.StringAttribute{Computed: true, Description: "Instance mode (prototyping or production)."},
						"ram_per_vcpu_gib":  schema.Int64Attribute{Computed: true, Description: "RAM per vCPU in GiB."},
						"vram_gb":           schema.Int64Attribute{Computed: true, Description: "GPU VRAM in GB."},
						"max_cpu_per_gpu":   schema.Int64Attribute{Computed: true, Description: "Maximum vCPUs per GPU."},
						"storage_min_gb":    schema.Int64Attribute{Computed: true, Description: "Minimum disk size in GB."},
						"storage_max_gb":    schema.Int64Attribute{Computed: true, Description: "Maximum disk size in GB."},
					},
				},
			},
		},
	}
}

func (d *GPUSpecsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Error configuring Thunder Compute GPU specs data source",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *GPUSpecsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	specs, err := d.client.GetGPUSpecs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Thunder Compute GPU specs", err.Error())
		return
	}

	model := GPUSpecsDataSourceModel{
		Specs: make(map[string]GPUSpecModel, len(specs)),
	}
	for key, spec := range specs {
		model.Specs[key] = GPUSpecModel{
			DisplayName:   types.StringValue(spec.DisplayName),
			GPUCount:      types.Int64Value(int64(spec.GPUCount)),
			Mode:          types.StringValue(spec.Mode),
			RAMPerVCPUGiB: types.Int64Value(int64(spec.RAMPerVCPUGiB)),
			VRAMGB:        types.Int64Value(int64(spec.VRAMGB)),
			MaxCPUPerGPU:  types.Int64Value(int64(spec.Limits.MaxCPUPerGPU)),
			StorageMinGB:  types.Int64Value(int64(spec.StorageGB.Min)),
			StorageMaxGB:  types.Int64Value(int64(spec.StorageGB.Max)),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
