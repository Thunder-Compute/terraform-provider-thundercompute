package resources

import (
	"context"
	"math/big"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestResolveInstanceMode(t *testing.T) {
	tests := []struct {
		name              string
		configuredMode    string
		configuredModeSet bool
		stateMode         string
		planGPUs          int64
		stateGPUs         int64
		hasState          bool
		wantMode          string
		wantWarning       bool
		wantError         bool
	}{
		{name: "omitted new one GPU", planGPUs: 1, wantMode: "prototyping"},
		{name: "omitted new two GPUs", planGPUs: 2, wantMode: "prototyping"},
		{name: "omitted new four GPUs", planGPUs: 4, wantMode: "production"},
		{name: "omitted new eight GPUs", planGPUs: 8, wantMode: "production"},
		{
			name:              "matching explicit mode on new resource",
			configuredMode:    "production",
			configuredModeSet: true,
			planGPUs:          4,
			wantMode:          "production",
		},
		{
			name:              "conflicting explicit mode on new resource",
			configuredMode:    "production",
			configuredModeSet: true,
			planGPUs:          1,
			wantError:         true,
		},
		{
			name:              "preserved conflicting legacy config and state",
			configuredMode:    "production",
			configuredModeSet: true,
			stateMode:         "production",
			planGPUs:          1,
			stateGPUs:         1,
			hasState:          true,
			wantMode:          "production",
			wantWarning:       true,
		},
		{
			name:        "preserved conflicting legacy state when mode omitted",
			stateMode:   "production",
			planGPUs:    1,
			stateGPUs:   1,
			hasState:    true,
			wantMode:    "production",
			wantWarning: true,
		},
		{
			name:      "GPU count transition refreshes derived mode",
			stateMode: "prototyping",
			planGPUs:  4,
			stateGPUs: 2,
			hasState:  true,
			wantMode:  "production",
		},
		{
			name:      "invalid GPU count",
			planGPUs:  3,
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotMode, warning, err := resolveInstanceMode(
				tt.configuredMode,
				tt.configuredModeSet,
				tt.stateMode,
				tt.planGPUs,
				tt.stateGPUs,
				tt.hasState,
			)
			if (err != nil) != tt.wantError {
				t.Fatalf("resolveInstanceMode() error = %v, wantError %t", err, tt.wantError)
			}
			if tt.wantError {
				return
			}
			if gotMode != tt.wantMode {
				t.Errorf("mode = %q, want %q", gotMode, tt.wantMode)
			}
			if (warning != "") != tt.wantWarning {
				t.Errorf("warning = %q, wantWarning %t", warning, tt.wantWarning)
			}
		})
	}
}

func TestInstanceModifyPlanDerivesAndValidatesMode(t *testing.T) {
	ctx := context.Background()
	r := &InstanceResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema

	tests := []struct {
		name           string
		configuredMode interface{}
		planGPUs       int64
		stateMode      interface{}
		stateGPUs      interface{}
		planDisk       int64
		stateDisk      interface{}
		wantMode       string
		wantWarning    bool
		wantError      bool
	}{
		{
			name:           "omitted mode is derived",
			configuredMode: nil,
			planGPUs:       4,
			planDisk:       100,
			wantMode:       "production",
		},
		{
			name:           "matching mode is retained",
			configuredMode: "prototyping",
			planGPUs:       2,
			planDisk:       100,
			wantMode:       "prototyping",
		},
		{
			name:           "conflicting new mode is rejected",
			configuredMode: "production",
			planGPUs:       1,
			planDisk:       100,
			wantError:      true,
		},
		{
			name:           "conflicting legacy mode is preserved",
			configuredMode: "production",
			planGPUs:       1,
			stateMode:      "production",
			stateGPUs:      int64(1),
			planDisk:       100,
			stateDisk:      int64(100),
			wantMode:       "production",
			wantWarning:    true,
		},
		{
			name:           "disk decrease is rejected",
			configuredMode: nil,
			planGPUs:       1,
			stateMode:      "prototyping",
			stateGPUs:      int64(1),
			planDisk:       99,
			stateDisk:      int64(100),
			wantError:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
				"mode":         tt.configuredMode,
				"num_gpus":     tt.planGPUs,
				"disk_size_gb": tt.planDisk,
			})
			planRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
				"mode":         tt.configuredMode,
				"num_gpus":     tt.planGPUs,
				"disk_size_gb": tt.planDisk,
			})
			stateRaw := tftypes.NewValue(s.Type().TerraformType(ctx), nil)
			if tt.stateGPUs != nil {
				stateRaw = instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
					"mode":         tt.stateMode,
					"num_gpus":     tt.stateGPUs,
					"disk_size_gb": tt.stateDisk,
				})
			}

			req := resource.ModifyPlanRequest{
				Config: tfsdk.Config{Raw: configRaw, Schema: s},
				Plan:   tfsdk.Plan{Raw: planRaw, Schema: s},
				State:  tfsdk.State{Raw: stateRaw, Schema: s},
			}
			var resp resource.ModifyPlanResponse
			r.ModifyPlan(ctx, req, &resp)
			if resp.Diagnostics.HasError() != tt.wantError {
				t.Fatalf("ModifyPlan() diagnostics = %v, wantError %t", resp.Diagnostics, tt.wantError)
			}
			if tt.wantError {
				return
			}
			var gotMode types.String
			diags := resp.Plan.GetAttribute(ctx, path.Root("mode"), &gotMode)
			if diags.HasError() {
				t.Fatalf("reading planned mode: %v", diags)
			}
			if gotMode.ValueString() != tt.wantMode {
				t.Errorf("planned mode = %q, want %q", gotMode.ValueString(), tt.wantMode)
			}
			if (resp.Diagnostics.WarningsCount() > 0) != tt.wantWarning {
				t.Errorf("warnings = %v, wantWarning %t", resp.Diagnostics, tt.wantWarning)
			}
		})
	}
}

func TestInstanceImportWarnsAboutDerivedMode(t *testing.T) {
	ctx := context.Background()
	r := &InstanceResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	resp := resource.ImportStateResponse{State: tfsdk.State{
		Schema: schemaResp.Schema,
		Raw:    tftypes.NewValue(schemaResp.Schema.Type().TerraformType(ctx), nil),
	}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "instance-uuid"}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("ImportState() diagnostics: %v", resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("ImportState() warnings = %d, want 1", resp.Diagnostics.WarningsCount())
	}
	var gotID types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("id"), &gotID); diags.HasError() {
		t.Fatalf("reading imported id: %v", diags)
	}
	if gotID.ValueString() != "instance-uuid" {
		t.Errorf("imported id = %q, want instance-uuid", gotID.ValueString())
	}
}

func instancePlanningValue(ctx context.Context, t *testing.T, terraformType tftypes.Type, overrides map[string]interface{}) tftypes.Value {
	t.Helper()
	objectType := terraformType.(tftypes.Object)
	values := make(map[string]tftypes.Value, len(objectType.AttributeTypes))
	for name, attributeType := range objectType.AttributeTypes {
		values[name] = tftypes.NewValue(attributeType, nil)
	}
	for name, value := range overrides {
		attributeType, ok := objectType.AttributeTypes[name]
		if !ok {
			t.Fatalf("unknown instance attribute %q", name)
		}
		if number, ok := value.(int64); ok {
			value = big.NewFloat(float64(number))
		}
		if numbers, ok := value.([]int64); ok {
			setType := attributeType.(tftypes.Set)
			setValues := make([]tftypes.Value, len(numbers))
			for i, number := range numbers {
				setValues[i] = tftypes.NewValue(setType.ElementType, big.NewFloat(float64(number)))
			}
			value = setValues
		}
		values[name] = tftypes.NewValue(attributeType, value)
	}
	return tftypes.NewValue(terraformType, values)
}
