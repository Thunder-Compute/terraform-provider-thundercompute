package datasources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"terraform-provider-thundercompute/internal/client"
)

var _ datasource.DataSource = (*PricingDataSource)(nil)

type PricingDataSource struct {
	client *client.Client
}

type PricingDataSourceModel struct {
	Pricing map[string]types.Float64 `tfsdk:"pricing"`
}

func NewPricingDataSource() datasource.DataSource {
	return &PricingDataSource{}
}

func (d *PricingDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pricing"
}

func (d *PricingDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Retrieves current hourly pricing for Thunder Compute GPU types.",
		Attributes: map[string]schema.Attribute{
			"pricing": schema.MapAttribute{
				Computed:    true,
				ElementType: types.Float64Type,
				Description: "Map of GPU type/mode identifier to hourly price in USD.",
			},
		},
	}
}

func (d *PricingDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Error configuring Thunder Compute pricing data source",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *PricingDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	pricing, err := d.client.GetPricing(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Thunder Compute pricing", err.Error())
		return
	}

	model := PricingDataSourceModel{
		Pricing: make(map[string]types.Float64, len(pricing)),
	}
	for key, price := range pricing {
		model.Pricing[key] = types.Float64Value(price)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
