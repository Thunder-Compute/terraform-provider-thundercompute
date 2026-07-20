package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestInstanceStateUpgradeV0PreservesSupportedStateAndDropsRemovedFields(t *testing.T) {
	ctx := context.Background()
	r := &InstanceResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	if schemaResp.Schema.Version != 1 {
		t.Fatalf("schema version = %d, want 1", schemaResp.Schema.Version)
	}
	for _, removed := range []string{"mode", "allow_snapshot_modify"} {
		if _, ok := schemaResp.Schema.Attributes[removed]; ok {
			t.Errorf("current schema still contains removed attribute %q", removed)
		}
	}

	upgrader, ok := r.UpgradeState(ctx)[0]
	if !ok {
		t.Fatal("missing v0 state upgrader")
	}
	if upgrader.PriorSchema == nil {
		t.Fatal("v0 state upgrader has no prior schema")
	}

	priorType := upgrader.PriorSchema.Type().TerraformType(ctx)
	priorRaw := instancePlanningValue(ctx, t, priorType, map[string]interface{}{
		"gpu_type":              "A6000",
		"template":              "legacy-snapshot",
		"mode":                  "production",
		"cpu_cores":             int64(24),
		"disk_size_gb":          int64(321),
		"num_gpus":              int64(1),
		"public_key":            "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFixture user@example.com",
		"http_ports":            []int64{8080, 8443},
		"allow_snapshot_modify": true,
		"id":                    "instance-uuid",
		"identifier":            int64(17),
		"generated_key":         "-----BEGIN OPENSSH PRIVATE KEY-----\nfixture\n-----END OPENSSH PRIVATE KEY-----",
		"status":                "RUNNING",
		"ip":                    "203.0.113.42",
		"port":                  int64(22022),
		"name":                  "legacy-instance",
		"memory":                "96GiB",
		"created_at":            "2025-01-02T03:04:05Z",
		"ssh_public_keys": []tftypes.Value{
			tftypes.NewValue(tftypes.String, "ssh-ed25519 key-one"),
			tftypes.NewValue(tftypes.String, "ssh-rsa key-two"),
		},
		"timeouts": map[string]tftypes.Value{
			"create": tftypes.NewValue(tftypes.String, "11m"),
			"update": tftypes.NewValue(tftypes.String, "22m"),
			"delete": tftypes.NewValue(tftypes.String, "3m"),
		},
	})

	var upgradeResp resource.UpgradeStateResponse
	upgradeResp.State.Schema = schemaResp.Schema
	upgrader.StateUpgrader(ctx, resource.UpgradeStateRequest{
		State: &tfsdk.State{Raw: priorRaw, Schema: *upgrader.PriorSchema},
	}, &upgradeResp)
	if upgradeResp.Diagnostics.HasError() {
		t.Fatalf("upgrade diagnostics: %v", upgradeResp.Diagnostics)
	}
	if upgradeResp.Diagnostics.WarningsCount() != 0 {
		t.Fatalf("upgrade warnings: %v", upgradeResp.Diagnostics)
	}
	if upgradeResp.State.Raw.IsNull() {
		t.Fatal("upgrader returned no state")
	}

	var priorValues, upgradedValues map[string]tftypes.Value
	if err := priorRaw.As(&priorValues); err != nil {
		t.Fatalf("converting v0 state: %v", err)
	}
	if err := upgradeResp.State.Raw.As(&upgradedValues); err != nil {
		t.Fatalf("converting upgraded state: %v", err)
	}
	for name := range schemaResp.Schema.Attributes {
		priorValue, ok := priorValues[name]
		if !ok {
			t.Errorf("current attribute %q has no v0 value", name)
			continue
		}
		if !upgradedValues[name].Equal(priorValue) {
			t.Errorf("attribute %q changed during upgrade: got %s, want %s", name, upgradedValues[name], priorValue)
		}
	}
	for _, removed := range []string{"mode", "allow_snapshot_modify"} {
		if _, ok := upgradedValues[removed]; ok {
			t.Errorf("upgraded state still contains removed attribute %q", removed)
		}
	}
}
