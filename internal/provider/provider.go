package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"terraform-provider-thundercompute/internal/client"
	"terraform-provider-thundercompute/internal/datasources"
	"terraform-provider-thundercompute/internal/resources"
)

var _ provider.Provider = (*ThunderComputeProvider)(nil)

type ThunderComputeProvider struct {
	version string
}

type ThunderComputeProviderModel struct {
	APIToken types.String `tfsdk:"api_token"`
	APIURL   types.String `tfsdk:"api_url"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &ThunderComputeProvider{version: version}
	}
}

func (p *ThunderComputeProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "thundercompute"
	resp.Version = p.version
}

func (p *ThunderComputeProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Terraform provider for managing Thunder Compute GPU cloud resources.",
		Attributes: map[string]schema.Attribute{
			"api_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Thunder Compute API token. Can also be set via the TNR_API_TOKEN environment variable.",
			},
			"api_url": schema.StringAttribute{
				Optional:    true,
				Description: "Thunder Compute API base URL. Defaults to https://api.thundercompute.com:8443/v1",
			},
		},
	}
}

func (p *ThunderComputeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config ThunderComputeProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Defer configuration if token is unknown (e.g. computed from another resource)
	if config.APIToken.IsUnknown() {
		resp.Diagnostics.AddWarning(
			"Provider configuration deferred",
			"api_token is not yet known. Provider will be configured when the value becomes available.",
		)
		return
	}

	// Resolve API token: config attribute takes precedence over env var
	apiToken := config.APIToken.ValueString()
	if apiToken == "" {
		apiToken = os.Getenv("TNR_API_TOKEN")
	}
	if apiToken == "" {
		resp.Diagnostics.AddError(
			"Missing API Token",
			"Set the api_token provider attribute or the TNR_API_TOKEN environment variable.",
		)
		return
	}

	if config.APIURL.IsUnknown() {
		resp.Diagnostics.AddWarning(
			"Provider configuration deferred",
			"api_url is not yet known. Provider will be configured when the value becomes available.",
		)
		return
	}
	apiURL := config.APIURL.ValueString()

	c := client.NewClient(apiURL, apiToken, p.version)
	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *ThunderComputeProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		resources.NewInstanceResource,
		resources.NewInstanceKeyResource,
		resources.NewSSHKeyResource,
		resources.NewSnapshotResource,
	}
}

func (p *ThunderComputeProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		datasources.NewInstancesDataSource,
		datasources.NewGPUSpecsDataSource,
		datasources.NewPricingDataSource,
		datasources.NewTemplatesDataSource,
	}
}
