package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestUnknownStringOnConfigChange_Description(t *testing.T) {
	m := UnknownStringOnConfigChange()
	desc := m.Description(context.Background())
	if desc == "" {
		t.Error("expected non-empty description")
	}
	md := m.MarkdownDescription(context.Background())
	if md != desc {
		t.Errorf("MarkdownDescription = %q, want %q", md, desc)
	}
}

func TestUnknownInt64OnConfigChange_Description(t *testing.T) {
	m := UnknownInt64OnConfigChange()
	desc := m.Description(context.Background())
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

func TestUnknownListOnConfigChange_Description(t *testing.T) {
	m := UnknownListOnConfigChange()
	desc := m.Description(context.Background())
	if desc == "" {
		t.Error("expected non-empty description")
	}
}

func TestConfigTriggerAttributes_NotEmpty(t *testing.T) {
	if len(configTriggerAttributes) == 0 {
		t.Error("configTriggerAttributes should not be empty")
	}
	expected := map[string]bool{
		"cpu_cores": true, "disk_size_gb": true, "gpu_type": true,
		"num_gpus": true, "http_ports": true,
		"template": true, "public_key": true,
	}
	if len(configTriggerAttributes) != len(expected) {
		t.Fatalf("configTriggerAttributes = %v, want exactly %v", configTriggerAttributes, expected)
	}
	for _, attr := range configTriggerAttributes {
		if !expected[attr] {
			t.Errorf("unexpected trigger attribute: %q", attr)
		}
	}
}

func TestUnknownStringOnInstanceReplacement(t *testing.T) {
	ctx := context.Background()
	r := &InstanceResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	terraType := s.Type().TerraformType(ctx)

	stateRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"cpu_cores": int64(4),
		"template":  "base",
	})
	tests := []struct {
		name        string
		overrides   map[string]interface{}
		wantUnknown bool
	}{
		{
			name: "in-place update preserves identity",
			overrides: map[string]interface{}{
				"cpu_cores": int64(8),
				"template":  "base",
			},
		},
		{
			name: "template replacement invalidates identity",
			overrides: map[string]interface{}{
				"cpu_cores": int64(4),
				"template":  "snapshot",
			},
			wantUnknown: true,
		},
		{
			name: "public key replacement invalidates identity",
			overrides: map[string]interface{}{
				"cpu_cores":  int64(4),
				"template":   "base",
				"public_key": "ssh-ed25519 AAAAAAAAAAA test@example.com",
			},
			wantUnknown: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			planRaw := instancePlanningValue(ctx, t, terraType, tt.overrides)
			req := planmodifier.StringRequest{
				Plan:       tfsdk.Plan{Raw: planRaw, Schema: s},
				PlanValue:  types.StringUnknown(),
				State:      tfsdk.State{Raw: stateRaw, Schema: s},
				StateValue: types.StringValue("instance-uuid"),
			}
			resp := planmodifier.StringResponse{PlanValue: req.PlanValue}
			UnknownStringOnInstanceReplacement().PlanModifyString(ctx, req, &resp)

			if resp.PlanValue.IsUnknown() != tt.wantUnknown {
				t.Fatalf("planned ID = %v, wantUnknown %t", resp.PlanValue, tt.wantUnknown)
			}
			if !tt.wantUnknown && resp.PlanValue.ValueString() != "instance-uuid" {
				t.Errorf("planned ID = %q, want instance-uuid", resp.PlanValue.ValueString())
			}
		})
	}
}
