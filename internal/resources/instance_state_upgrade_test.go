package resources

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestInstanceStateUpgradeV0PreservesOrPopulatesLegacyMode(t *testing.T) {
	ctx := context.Background()
	r := &InstanceResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Schema.Version != 1 {
		t.Fatalf("schema version = %d, want 1", schemaResp.Schema.Version)
	}

	upgrader, ok := r.UpgradeState(ctx)[0]
	if !ok {
		t.Fatal("missing v0 state upgrader")
	}

	tests := []struct {
		name     string
		mode     *string
		numGPUs  int64
		wantMode string
	}{
		{name: "preserves non-empty legacy mode", mode: stringPointer("production"), numGPUs: 1, wantMode: "production"},
		{name: "populates missing compatibility value", mode: nil, numGPUs: 4, wantMode: "production"},
	}

	schemaType := schemaResp.Schema.Type().TerraformType(ctx)
	objectType := schemaType.(tftypes.Object)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rawValues := make(map[string]interface{}, len(objectType.AttributeTypes))
			for name := range objectType.AttributeTypes {
				rawValues[name] = nil
			}
			rawValues["mode"] = tt.mode
			rawValues["num_gpus"] = tt.numGPUs
			rawJSON, err := json.Marshal(rawValues)
			if err != nil {
				t.Fatalf("creating raw v0 state: %v", err)
			}
			var upgradeResp resource.UpgradeStateResponse
			upgrader.StateUpgrader(ctx, resource.UpgradeStateRequest{
				RawState: &tfprotov6.RawState{JSON: rawJSON},
			}, &upgradeResp)
			if upgradeResp.Diagnostics.HasError() {
				t.Fatalf("upgrade diagnostics: %v", upgradeResp.Diagnostics)
			}
			if upgradeResp.DynamicValue == nil {
				t.Fatal("upgrader returned no dynamic value")
			}

			upgraded, err := upgradeResp.DynamicValue.Unmarshal(schemaType)
			if err != nil {
				t.Fatalf("unmarshaling upgraded state: %v", err)
			}
			var upgradedValues map[string]tftypes.Value
			if err := upgraded.As(&upgradedValues); err != nil {
				t.Fatalf("converting upgraded state: %v", err)
			}
			var gotMode string
			if err := upgradedValues["mode"].As(&gotMode); err != nil {
				t.Fatalf("converting upgraded mode: %v", err)
			}
			if gotMode != tt.wantMode {
				t.Errorf("mode = %q, want %q", gotMode, tt.wantMode)
			}
		})
	}
}

func stringPointer(value string) *string {
	return &value
}
