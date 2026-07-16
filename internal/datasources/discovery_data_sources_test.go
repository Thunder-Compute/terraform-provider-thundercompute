package datasources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"

	"terraform-provider-thundercompute/internal/client"
)

func TestDiscoveryDataSourcesUseCanonicalV2Contracts(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/specs", func(w http.ResponseWriter, _ *http.Request) {
		writeDataSourceJSON(t, w, map[string]interface{}{"specs": map[string]interface{}{
			"a6000_x1": map[string]interface{}{
				"displayName": "NVIDIA RTX A6000", "gpuCount": 1,
				"ramCapGiB": 96, "ramPerVCPUGiB": 4, "vramGB": 48,
				"vcpuOptions": []int{4, 8}, "storageGB": map[string]int{"min": 50, "max": 200},
			},
		}})
	})
	mux.HandleFunc("/v2/status", func(w http.ResponseWriter, _ *http.Request) {
		writeDataSourceJSON(t, w, map[string]interface{}{
			"gpu_type": map[string]interface{}{"a6000": map[string]string{"status": "available"}},
			"specs":    map[string]string{"a6000_x1": "available"},
		})
	})
	mux.HandleFunc("/v2/pricing", func(w http.ResponseWriter, _ *http.Request) {
		writeDataSourceJSON(t, w, map[string]interface{}{"pricing": map[string]float64{"a6000_x1": 0.35}})
	})
	mux.HandleFunc("/v1/instances/list", func(w http.ResponseWriter, _ *http.Request) {
		writeDataSourceJSON(t, w, map[string]interface{}{"0": map[string]interface{}{
			"uuid": "instance-uuid", "name": "instance", "status": "RUNNING",
			"gpuType": "A6000", "cpuCores": "4", "numGpus": "4", "storage": 100,
			"ip": "203.0.113.1", "port": 2222,
		}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	c := client.NewClient(server.URL, "test-token", "test")

	t.Run("GPU specs", func(t *testing.T) {
		d := &GPUSpecsDataSource{client: c}
		var schemaResp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
		resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
		d.Read(ctx, datasource.ReadRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read() diagnostics: %v", resp.Diagnostics)
		}
		var got GPUSpecsDataSourceModel
		if diags := resp.State.Get(ctx, &got); diags.HasError() {
			t.Fatalf("reading GPU specs state: %v", diags)
		}
		spec := got.Specs["a6000_x1"]
		if spec.RAMCapGiB.ValueInt64() != 96 {
			t.Errorf("ram_cap_gib = %d, want 96", spec.RAMCapGiB.ValueInt64())
		}
		if spec.Mode.ValueString() != "prototyping" {
			t.Errorf("legacy mode compatibility value = %q, want prototyping", spec.Mode.ValueString())
		}
		if _, found := got.Specs["a6000_x1_prototyping"]; found {
			t.Error("state unexpectedly contains legacy mode-suffixed key")
		}
	})

	t.Run("availability", func(t *testing.T) {
		d := &GPUAvailabilityDataSource{client: c}
		var schemaResp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
		resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
		d.Read(ctx, datasource.ReadRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read() diagnostics: %v", resp.Diagnostics)
		}
		var got GPUAvailabilityDataSourceModel
		if diags := resp.State.Get(ctx, &got); diags.HasError() {
			t.Fatalf("reading availability state: %v", diags)
		}
		if got.Specs["a6000_x1"].ValueString() != "available" {
			t.Errorf("availability = %v, want a6000_x1 available", got.Specs)
		}
	})

	t.Run("pricing", func(t *testing.T) {
		d := &PricingDataSource{client: c}
		var schemaResp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
		resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
		d.Read(ctx, datasource.ReadRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read() diagnostics: %v", resp.Diagnostics)
		}
		var got PricingDataSourceModel
		if diags := resp.State.Get(ctx, &got); diags.HasError() {
			t.Fatalf("reading pricing state: %v", diags)
		}
		if got.Pricing["a6000_x1"].ValueFloat64() != 0.35 {
			t.Errorf("pricing = %v, want a6000_x1 0.35", got.Pricing)
		}
	})

	t.Run("instances retains list output and legacy compatibility", func(t *testing.T) {
		d := &InstancesDataSource{client: c}
		var schemaResp datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
		resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
		d.Read(ctx, datasource.ReadRequest{}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read() diagnostics: %v", resp.Diagnostics)
		}
		var got InstancesDataSourceModel
		if diags := resp.State.Get(ctx, &got); diags.HasError() {
			t.Fatalf("reading instances state: %v", diags)
		}
		if len(got.Instances) != 1 {
			t.Fatalf("instances list length = %d, want 1", len(got.Instances))
		}
		if got.Instances[0].Mode.ValueString() != "production" {
			t.Errorf("legacy mode compatibility value = %q, want production", got.Instances[0].Mode.ValueString())
		}
	})
}

func TestTemplatesDataSourcePreservesMissingDefaultsAsNull(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/thunder-templates", func(w http.ResponseWriter, _ *http.Request) {
		writeDataSourceJSON(t, w, map[string]interface{}{"base": map[string]interface{}{"displayName": "Base"}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	d := &TemplatesDataSource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp datasource.SchemaResponse
	d.Schema(ctx, datasource.SchemaRequest{}, &schemaResp)
	resp := datasource.ReadResponse{State: tfsdk.State{Schema: schemaResp.Schema}}
	d.Read(ctx, datasource.ReadRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read() diagnostics: %v", resp.Diagnostics)
	}
	var got TemplatesDataSourceModel
	if diags := resp.State.Get(ctx, &got); diags.HasError() {
		t.Fatalf("reading templates state: %v", diags)
	}
	template := got.Templates["base"]
	if !template.DefaultGPUType.IsNull() || !template.DefaultCores.IsNull() || !template.DefaultStorage.IsNull() || !template.DefaultNumGPUs.IsNull() {
		t.Errorf("missing template defaults were collapsed instead of null: %+v", template)
	}
}

func writeDataSourceJSON(t *testing.T, w http.ResponseWriter, value interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encoding response: %v", err)
	}
}
