package datasources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"terraform-provider-thundercompute/internal/client"
)

var _ datasource.DataSource = (*GPUAvailabilityDataSource)(nil)

type GPUAvailabilityDataSource struct {
	client *client.Client
}

type GPUAvailabilityDataSourceModel struct {
	Specs map[string]types.String `tfsdk:"specs"`
}

func NewGPUAvailabilityDataSource() datasource.DataSource {
	return &GPUAvailabilityDataSource{}
}

func (d *GPUAvailabilityDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_gpu_availability"
}

func (d *GPUAvailabilityDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Retrieves current GPU availability status for all Thunder Compute configurations. " +
			"Each key matches the spec keys from the thundercompute_gpu_specs data source (e.g. \"h100_x1_production\"). " +
			"Values are \"available\" or \"unavailable\".",
		Attributes: map[string]schema.Attribute{
			"specs": schema.MapAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Map of spec configuration key to availability status (\"available\" or \"unavailable\").",
			},
		},
	}
}

func (d *GPUAvailabilityDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Error configuring Thunder Compute GPU availability data source",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *GPUAvailabilityDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	availability, err := d.client.GetGPUAvailability(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Thunder Compute GPU availability", err.Error())
		return
	}

	model := GPUAvailabilityDataSourceModel{
		Specs: make(map[string]types.String),
	}

	if availability.Specs != nil {
		for key, status := range availability.Specs {
			model.Specs[key] = types.StringValue(status)
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
