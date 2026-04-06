package datasources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"terraform-provider-thundercompute/internal/client"
)

var _ datasource.DataSource = (*TemplatesDataSource)(nil)

type TemplatesDataSource struct {
	client *client.Client
}

type TemplatesDataSourceModel struct {
	Templates map[string]TemplateModel `tfsdk:"templates"`
}

type TemplateModel struct {
	DisplayName         types.String `tfsdk:"display_name"`
	ExtendedDescription types.String `tfsdk:"extended_description"`
	IsDefault           types.Bool   `tfsdk:"is_default"`
	StartupMinutes      types.Int64  `tfsdk:"startup_minutes"`
	Version             types.Int64  `tfsdk:"version"`
	DefaultGPUType      types.String `tfsdk:"default_gpu_type"`
	DefaultCores        types.Int64  `tfsdk:"default_cores"`
	DefaultStorage      types.Int64  `tfsdk:"default_storage"`
	DefaultNumGPUs      types.Int64  `tfsdk:"default_num_gpus"`
}

func NewTemplatesDataSource() datasource.DataSource {
	return &TemplatesDataSource{}
}

func (d *TemplatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_templates"
}

func (d *TemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Lists available Thunder Compute instance templates.",
		Attributes: map[string]schema.Attribute{
			"templates": schema.MapNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"display_name":         schema.StringAttribute{Computed: true, Description: "Human-readable template name."},
						"extended_description": schema.StringAttribute{Computed: true, Description: "Detailed template description."},
						"is_default":           schema.BoolAttribute{Computed: true, Description: "Whether this is the default template."},
						"startup_minutes":      schema.Int64Attribute{Computed: true, Description: "Estimated startup time in minutes."},
						"version":              schema.Int64Attribute{Computed: true, Description: "Template version number."},
						"default_gpu_type":     schema.StringAttribute{Computed: true, Description: "Default GPU type for this template."},
						"default_cores":        schema.Int64Attribute{Computed: true, Description: "Default vCPU core count."},
						"default_storage":      schema.Int64Attribute{Computed: true, Description: "Default storage in GB."},
						"default_num_gpus":     schema.Int64Attribute{Computed: true, Description: "Default number of GPUs."},
					},
				},
			},
		},
	}
}

func (d *TemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Error configuring Thunder Compute templates data source",
			fmt.Sprintf("Expected *client.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData))
		return
	}
	d.client = c
}

func (d *TemplatesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	templates, err := d.client.GetTemplates(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Error reading Thunder Compute templates", err.Error())
		return
	}

	model := TemplatesDataSourceModel{
		Templates: make(map[string]TemplateModel, len(templates)),
	}
	for key, tmpl := range templates {
		m := TemplateModel{
			DisplayName:         types.StringValue(tmpl.DisplayName),
			ExtendedDescription: types.StringValue(tmpl.ExtendedDescription),
			IsDefault:           types.BoolValue(tmpl.Default),
			StartupMinutes:      types.Int64Value(int64(tmpl.StartupMinutes)),
			Version:             types.Int64Value(int64(tmpl.Version)),
		}
		if tmpl.DefaultSpecs != nil {
			m.DefaultGPUType = types.StringValue(tmpl.DefaultSpecs.GPUType)
			m.DefaultCores = types.Int64Value(int64(tmpl.DefaultSpecs.Cores))
			m.DefaultStorage = types.Int64Value(int64(tmpl.DefaultSpecs.Storage))
			m.DefaultNumGPUs = types.Int64Value(int64(tmpl.DefaultSpecs.NumGPUs))
		} else {
			m.DefaultGPUType = types.StringValue("")
			m.DefaultCores = types.Int64Value(0)
			m.DefaultStorage = types.Int64Value(0)
			m.DefaultNumGPUs = types.Int64Value(0)
		}
		model.Templates[key] = m
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &model)...)
}
