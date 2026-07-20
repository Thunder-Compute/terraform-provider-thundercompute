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

func TestInstanceModifyPlanRejectsDiskDecreaseUnlessReplacementPlanned(t *testing.T) {
	ctx := context.Background()
	r := &InstanceResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema

	tests := []struct {
		name           string
		planDisk       int64
		stateDisk      interface{}
		planTemplate   interface{}
		stateTemplate  interface{}
		planPublicKey  interface{}
		statePublicKey interface{}
		wantError      bool
	}{
		{name: "new resource may use any valid disk", planDisk: 50},
		{name: "unchanged disk is accepted", planDisk: 100, stateDisk: int64(100)},
		{name: "disk growth is accepted", planDisk: 101, stateDisk: int64(100)},
		{name: "disk decrease is rejected", planDisk: 99, stateDisk: int64(100), wantError: true},
		{
			name:          "template replacement allows a smaller disk",
			planDisk:      99,
			stateDisk:     int64(100),
			planTemplate:  "snapshot",
			stateTemplate: "base",
		},
		{
			name:           "public key replacement allows a smaller disk",
			planDisk:       99,
			stateDisk:      int64(100),
			planPublicKey:  "ssh-ed25519 AAAAAAAAAAA new@example.com",
			statePublicKey: "ssh-ed25519 AAAAAAAAAAA old@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
				"disk_size_gb": tt.planDisk,
				"template":     tt.planTemplate,
				"public_key":   tt.planPublicKey,
			})
			stateRaw := tftypes.NewValue(s.Type().TerraformType(ctx), nil)
			if tt.stateDisk != nil {
				stateRaw = instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
					"disk_size_gb": tt.stateDisk,
					"template":     tt.stateTemplate,
					"public_key":   tt.statePublicKey,
				})
			}

			req := resource.ModifyPlanRequest{
				Plan:  tfsdk.Plan{Raw: planRaw, Schema: s},
				State: tfsdk.State{Raw: stateRaw, Schema: s},
			}
			var resp resource.ModifyPlanResponse
			r.ModifyPlan(ctx, req, &resp)
			if resp.Diagnostics.HasError() != tt.wantError {
				t.Fatalf("ModifyPlan() diagnostics = %v, wantError %t", resp.Diagnostics, tt.wantError)
			}
			if resp.Diagnostics.WarningsCount() != 0 {
				t.Errorf("ModifyPlan() warnings = %v, want none", resp.Diagnostics)
			}
		})
	}
}

func TestInstanceImportSetsIDWithoutWarnings(t *testing.T) {
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
	if resp.Diagnostics.WarningsCount() != 0 {
		t.Fatalf("ImportState() warnings = %d, want 0", resp.Diagnostics.WarningsCount())
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
