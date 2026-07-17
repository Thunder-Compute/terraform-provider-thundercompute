package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"terraform-provider-thundercompute/internal/client"
)

func TestInstanceCreateRetainsUUIDAcrossVisibilityLagAndPatchesPorts(t *testing.T) {
	ctx := context.Background()
	originalPollInterval := instancePollInterval
	instancePollInterval = time.Millisecond
	defer func() { instancePollInterval = originalPollInterval }()
	listCalls := 0
	portPatchCalls := 0

	mux := http.NewServeMux()
	registerInstancePreflightFixtures(t, mux)
	mux.HandleFunc("/v1/instances/create", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Errorf("create method = %s, want POST", req.Method)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decoding create body: %v", err)
		}
		if _, ok := body["mode"]; ok {
			t.Error("create body unexpectedly contains mode")
		}
		writeResourceJSON(t, w, map[string]interface{}{
			"identifier": 0,
			"uuid":       "instance-uuid",
			"key":        "private-key-material",
		})
	})
	mux.HandleFunc("/v1/instances/list", func(w http.ResponseWriter, req *http.Request) {
		listCalls++
		if listCalls == 1 {
			writeResourceJSON(t, w, map[string]interface{}{})
			return
		}
		writeResourceJSON(t, w, map[string]interface{}{
			"0": runningInstanceFixture(nil),
		})
	})
	mux.HandleFunc("/v1/instances/0/ports", func(w http.ResponseWriter, req *http.Request) {
		portPatchCalls++
		if req.Method != http.MethodPatch {
			t.Errorf("port method = %s, want PATCH", req.Method)
		}
		var body client.PortUpdateRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decoding port body: %v", err)
		}
		if len(body.AddPorts) != 1 || body.AddPorts[0] != 8080 || len(body.RemovePorts) != 0 {
			t.Errorf("port body = %+v, want add [8080]", body)
		}
		writeResourceJSON(t, w, map[string]interface{}{
			"identifier":    "0",
			"instance_name": "instance-uuid",
			"http_ports":    []int{8080},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	raw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
		"gpu_type":              "A6000",
		"template":              "base",
		"mode":                  "prototyping",
		"cpu_cores":             int64(4),
		"disk_size_gb":          int64(100),
		"num_gpus":              int64(1),
		"http_ports":            []int64{8080},
		"allow_snapshot_modify": false,
	})
	req := resource.CreateRequest{
		Config: tfsdk.Config{Raw: raw, Schema: s},
		Plan:   tfsdk.Plan{Raw: raw, Schema: s},
	}
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, req, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create() diagnostics: %v", resp.Diagnostics)
	}

	var gotID, gotKey types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("id"), &gotID)...)
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("generated_key"), &gotKey)...)
	if resp.Diagnostics.HasError() {
		t.Fatalf("reading create state: %v", resp.Diagnostics)
	}
	if gotID.ValueString() != "instance-uuid" {
		t.Errorf("state id = %q, want instance-uuid", gotID.ValueString())
	}
	if gotKey.ValueString() != "private-key-material" {
		t.Errorf("generated key was not preserved")
	}
	if listCalls < 2 {
		t.Errorf("list calls = %d, want visibility retry", listCalls)
	}
	if portPatchCalls != 1 {
		t.Errorf("port PATCH calls = %d, want 1", portPatchCalls)
	}
}

// TestInstanceCreatePollsThroughTransientUnknownStatus guards against
// treating UNKNOWN as terminal: the API reports UNKNOWN whenever the control
// plane cannot determine state yet, including while a freshly created
// instance is still provisioning.
func TestInstanceCreatePollsThroughTransientUnknownStatus(t *testing.T) {
	ctx := context.Background()
	originalPollInterval := instancePollInterval
	instancePollInterval = time.Millisecond
	defer func() { instancePollInterval = originalPollInterval }()
	listCalls := 0

	mux := http.NewServeMux()
	registerInstancePreflightFixtures(t, mux)
	mux.HandleFunc("/v1/instances/create", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, map[string]interface{}{
			"identifier": 0,
			"uuid":       "instance-uuid",
			"key":        "private-key-material",
		})
	})
	mux.HandleFunc("/v1/instances/list", func(w http.ResponseWriter, _ *http.Request) {
		listCalls++
		if listCalls == 1 {
			unknown := runningInstanceFixture(nil)
			unknown["status"] = "UNKNOWN"
			unknown["ip"] = ""
			writeResourceJSON(t, w, map[string]interface{}{"0": unknown})
			return
		}
		writeResourceJSON(t, w, map[string]interface{}{"0": runningInstanceFixture(nil)})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	raw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
		"gpu_type":              "A6000",
		"template":              "base",
		"mode":                  "prototyping",
		"cpu_cores":             int64(4),
		"disk_size_gb":          int64(100),
		"num_gpus":              int64(1),
		"http_ports":            nil,
		"allow_snapshot_modify": false,
	})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{
		Config: tfsdk.Config{Raw: raw, Schema: s},
		Plan:   tfsdk.Plan{Raw: raw, Schema: s},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create() diagnostics: %v", resp.Diagnostics)
	}
	if listCalls < 2 {
		t.Errorf("list calls = %d, want poll through transient UNKNOWN", listCalls)
	}
	var gotStatus types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("status"), &gotStatus); diags.HasError() {
		t.Fatalf("reading status: %v", diags)
	}
	if gotStatus.ValueString() != "RUNNING" {
		t.Errorf("state status = %q, want RUNNING", gotStatus.ValueString())
	}
}

func TestInstanceCreatePartialStateContainsNoUnknownValues(t *testing.T) {
	ctx := context.Background()
	createCalls := 0
	mux := http.NewServeMux()
	registerInstancePreflightFixtures(t, mux)
	mux.HandleFunc("/v1/instances/create", func(w http.ResponseWriter, _ *http.Request) {
		createCalls++
		writeResourceJSON(t, w, map[string]interface{}{
			"identifier": 7,
			"uuid":       "instance-uuid",
			"key":        "private-key-material",
		})
	})
	mux.HandleFunc("/v1/instances/list", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		writeResourceJSON(t, w, map[string]interface{}{
			"error":   "not_found",
			"message": "instance list temporarily unavailable",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	configValues := map[string]interface{}{
		"gpu_type": "A6000", "template": "base", "mode": "prototyping",
		"cpu_cores": int64(4), "disk_size_gb": int64(100), "num_gpus": int64(1),
		"http_ports": nil, "allow_snapshot_modify": false,
	}
	planValues := cloneInterfaceMap(configValues)
	for _, name := range []string{
		"id", "identifier", "generated_key", "status", "ip", "port", "name",
		"memory", "created_at", "ssh_public_keys", "http_ports",
	} {
		planValues[name] = tftypes.UnknownValue
	}
	configRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), configValues)
	planRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), planValues)
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{
		Config: tfsdk.Config{Raw: configRaw, Schema: s},
		Plan:   tfsdk.Plan{Raw: planRaw, Schema: s},
	}, &resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("Create() returned no error after the post-create instance lookup failed")
	}
	if createCalls != 1 {
		t.Fatalf("instance create calls = %d, want 1", createCalls)
	}
	if !resp.State.Raw.IsFullyKnown() {
		t.Errorf("partial create state contains unknown values: %s", resp.State.Raw)
	}
	var gotID types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("id"), &gotID); diags.HasError() {
		t.Fatalf("reading partial create ID: %v", diags)
	}
	if gotID.ValueString() != "instance-uuid" {
		t.Errorf("partial create ID = %q, want instance-uuid", gotID.ValueString())
	}
}

func TestInstanceCreatePortSemantics(t *testing.T) {
	tests := []struct {
		name            string
		configuredPorts interface{}
		serverPorts     []int
		wantPatch       bool
		wantAdd         []int
		wantRemove      []int
		wantStatePorts  []int64
	}{
		{
			name:            "omitted ports adopt template defaults",
			configuredPorts: nil,
			serverPorts:     []int{80},
			wantStatePorts:  []int64{80},
		},
		{
			name:            "explicit empty removes template defaults",
			configuredPorts: []int64{},
			serverPorts:     []int{80},
			wantPatch:       true,
			wantRemove:      []int{80},
			wantStatePorts:  []int64{},
		},
		{
			name:            "explicit ports reconcile exactly",
			configuredPorts: []int64{8080},
			serverPorts:     []int{80},
			wantPatch:       true,
			wantAdd:         []int{8080},
			wantRemove:      []int{80},
			wantStatePorts:  []int64{8080},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			patchCalls := 0
			mux := http.NewServeMux()
			registerInstancePreflightFixtures(t, mux)
			mux.HandleFunc("/v1/instances/create", func(w http.ResponseWriter, _ *http.Request) {
				writeResourceJSON(t, w, map[string]interface{}{"identifier": 0, "uuid": "instance-uuid"})
			})
			mux.HandleFunc("/v1/instances/list", func(w http.ResponseWriter, _ *http.Request) {
				writeResourceJSON(t, w, map[string]interface{}{"0": runningInstanceFixture(tt.serverPorts)})
			})
			mux.HandleFunc("/v1/instances/0/ports", func(w http.ResponseWriter, req *http.Request) {
				patchCalls++
				var body client.PortUpdateRequest
				if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
					t.Errorf("decoding port body: %v", err)
				}
				if !intSliceEqual(body.AddPorts, tt.wantAdd) || !intSliceEqual(body.RemovePorts, tt.wantRemove) {
					t.Errorf("port body = %+v, want add %v remove %v", body, tt.wantAdd, tt.wantRemove)
				}
				writeResourceJSON(t, w, map[string]interface{}{
					"identifier": "0", "instance_name": "instance-uuid", "http_ports": int64sToInts(tt.wantStatePorts),
				})
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}
			var schemaResp resource.SchemaResponse
			r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
			s := schemaResp.Schema
			raw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
				"gpu_type": "A6000", "template": "base", "mode": "prototyping",
				"cpu_cores": int64(4), "disk_size_gb": int64(100), "num_gpus": int64(1),
				"http_ports": tt.configuredPorts, "allow_snapshot_modify": false,
			})
			resp := resource.CreateResponse{State: tfsdk.State{Schema: s}}
			r.Create(ctx, resource.CreateRequest{
				Config: tfsdk.Config{Raw: raw, Schema: s},
				Plan:   tfsdk.Plan{Raw: raw, Schema: s},
			}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Create() diagnostics: %v", resp.Diagnostics)
			}
			if (patchCalls > 0) != tt.wantPatch {
				t.Errorf("port PATCH calls = %d, wantPatch %t", patchCalls, tt.wantPatch)
			}
			var gotPorts types.Set
			diags := resp.State.GetAttribute(ctx, path.Root("http_ports"), &gotPorts)
			if diags.HasError() {
				t.Fatalf("reading ports state: %v", diags)
			}
			if !int64SliceSetEqual(extractInt64Set(gotPorts), tt.wantStatePorts) {
				t.Errorf("state ports = %v, want %v", extractInt64Set(gotPorts), tt.wantStatePorts)
			}
		})
	}
}

func TestInstanceUpdateSeparatesComputeAndPortOperations(t *testing.T) {
	ctx := context.Background()
	originalPollInterval := instancePollInterval
	instancePollInterval = time.Millisecond
	defer func() { instancePollInterval = originalPollInterval }()
	computeCalls := 0
	portCalls := 0
	computeModified := false
	postModifyListCalls := 0
	mux := http.NewServeMux()
	registerInstancePreflightFixtures(t, mux)
	mux.HandleFunc("/v1/instances/list", func(w http.ResponseWriter, _ *http.Request) {
		fixture := runningInstanceFixture([]int{80})
		if computeModified {
			postModifyListCalls++
			if postModifyListCalls > 1 {
				fixture["cpuCores"] = "8"
			}
		}
		writeResourceJSON(t, w, map[string]interface{}{"0": fixture})
	})
	mux.HandleFunc("/v1/instances/0/modify", func(w http.ResponseWriter, req *http.Request) {
		computeCalls++
		if req.Method != http.MethodPost {
			t.Errorf("compute method = %s, want POST", req.Method)
		}
		var body map[string]interface{}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decoding compute body: %v", err)
		}
		if body["cpu_cores"] != float64(8) {
			t.Errorf("compute body = %v, want cpu_cores 8", body)
		}
		for _, forbidden := range []string{"mode", "add_ports", "remove_ports"} {
			if _, ok := body[forbidden]; ok {
				t.Errorf("compute body unexpectedly contains %s", forbidden)
			}
		}
		computeModified = true
		writeResourceJSON(t, w, map[string]interface{}{"identifier": "0", "instance_name": "instance-uuid"})
	})
	mux.HandleFunc("/v1/instances/0/ports", func(w http.ResponseWriter, req *http.Request) {
		portCalls++
		var body client.PortUpdateRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decoding port body: %v", err)
		}
		if !intSliceEqual(body.AddPorts, []int{8080}) || !intSliceEqual(body.RemovePorts, []int{80}) {
			t.Errorf("port body = %+v, want add [8080] remove [80]", body)
		}
		writeResourceJSON(t, w, map[string]interface{}{
			"identifier": "0", "instance_name": "instance-uuid", "http_ports": []int{8080},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	common := map[string]interface{}{
		"gpu_type": "A6000", "template": "base", "mode": "prototyping",
		"disk_size_gb": int64(100), "num_gpus": int64(1), "allow_snapshot_modify": false,
	}
	stateValues := cloneInterfaceMap(common)
	stateValues["cpu_cores"] = int64(4)
	stateValues["http_ports"] = []int64{80}
	stateValues["id"] = "instance-uuid"
	stateValues["generated_key"] = "private-key-material"
	planValues := cloneInterfaceMap(common)
	planValues["cpu_cores"] = int64(8)
	planValues["http_ports"] = []int64{8080}
	planValues["id"] = "instance-uuid"
	planValues["generated_key"] = "private-key-material"
	stateRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), stateValues)
	planRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), planValues)
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s}}
	r.Update(ctx, resource.UpdateRequest{
		Config: tfsdk.Config{Raw: planRaw, Schema: s},
		Plan:   tfsdk.Plan{Raw: planRaw, Schema: s},
		State:  tfsdk.State{Raw: stateRaw, Schema: s},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update() diagnostics: %v", resp.Diagnostics)
	}
	if computeCalls != 1 || portCalls != 1 {
		t.Errorf("compute calls = %d, port calls = %d; want 1 each", computeCalls, portCalls)
	}
	var gotCPUCores types.Int64
	if diags := resp.State.GetAttribute(ctx, path.Root("cpu_cores"), &gotCPUCores); diags.HasError() {
		t.Fatalf("reading CPU cores: %v", diags)
	}
	if gotCPUCores.ValueInt64() != 8 {
		t.Errorf("state cpu_cores = %d, want 8 after stale-list convergence", gotCPUCores.ValueInt64())
	}
	var gotKey types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("generated_key"), &gotKey); diags.HasError() {
		t.Fatalf("reading generated key: %v", diags)
	}
	if gotKey.ValueString() != "private-key-material" {
		t.Error("generated private key was not preserved across update")
	}
}

func TestInstanceUpdateConfirmsTransientListMiss(t *testing.T) {
	ctx := context.Background()
	listCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/instances/list", func(w http.ResponseWriter, _ *http.Request) {
		listCalls++
		if listCalls == 1 {
			writeResourceJSON(t, w, map[string]interface{}{})
			return
		}
		writeResourceJSON(t, w, map[string]interface{}{"0": runningInstanceFixture(nil)})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	values := map[string]interface{}{
		"gpu_type": "A6000", "template": "base", "mode": "prototyping",
		"cpu_cores": int64(4), "disk_size_gb": int64(100), "num_gpus": int64(1),
		"http_ports": nil, "allow_snapshot_modify": false, "id": "instance-uuid",
		"generated_key": "private-key-material",
	}
	raw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), values)
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s}}
	r.Update(ctx, resource.UpdateRequest{
		Config: tfsdk.Config{Raw: raw, Schema: s},
		Plan:   tfsdk.Plan{Raw: raw, Schema: s},
		State:  tfsdk.State{Raw: raw, Schema: s},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update() diagnostics: %v", resp.Diagnostics)
	}
	if listCalls != 2 {
		t.Fatalf("list calls = %d, want 2 to confirm the transient miss", listCalls)
	}
	var gotID types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("id"), &gotID); diags.HasError() {
		t.Fatalf("reading instance id: %v", diags)
	}
	if gotID.ValueString() != "instance-uuid" {
		t.Errorf("state id = %q, want instance-uuid", gotID.ValueString())
	}
}

func TestInstanceDeleteConfirmsTransientListMiss(t *testing.T) {
	ctx := context.Background()
	listCalls := 0
	deleteCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/instances/list", func(w http.ResponseWriter, _ *http.Request) {
		listCalls++
		if listCalls == 1 {
			writeResourceJSON(t, w, map[string]interface{}{})
			return
		}
		writeResourceJSON(t, w, map[string]interface{}{"0": runningInstanceFixture(nil)})
	})
	mux.HandleFunc("/v1/instances/0/delete", func(w http.ResponseWriter, req *http.Request) {
		deleteCalls++
		if req.Method != http.MethodPost {
			t.Errorf("delete method = %s, want POST", req.Method)
		}
		writeResourceJSON(t, w, map[string]interface{}{"message": "deleted"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	raw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
		"gpu_type": "A6000", "template": "base", "mode": "prototyping",
		"cpu_cores": int64(4), "disk_size_gb": int64(100), "num_gpus": int64(1),
		"http_ports": nil, "allow_snapshot_modify": false, "id": "instance-uuid",
	})
	var resp resource.DeleteResponse
	r.Delete(ctx, resource.DeleteRequest{State: tfsdk.State{Raw: raw, Schema: s}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete() diagnostics: %v", resp.Diagnostics)
	}
	if listCalls != 2 {
		t.Errorf("list calls = %d, want 2 to confirm the transient miss", listCalls)
	}
	if deleteCalls != 1 {
		t.Errorf("delete calls = %d, want 1", deleteCalls)
	}
}

func TestInstanceUpdateUnsupportedVersionFailsClosed(t *testing.T) {
	ctx := context.Background()
	unexpectedMutations := 0
	mux := http.NewServeMux()
	registerInstancePreflightFixtures(t, mux)
	mux.HandleFunc("/v1/instances/list", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, map[string]interface{}{"0": runningInstanceFixture(nil)})
	})
	mux.HandleFunc("/v1/instances/0/modify", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		writeResourceJSON(t, w, map[string]interface{}{
			"error": "unsupported_instance_version", "message": "legacy instance cannot be modified",
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			unexpectedMutations++
		}
		http.NotFound(w, req)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	stateValues := map[string]interface{}{
		"gpu_type": "A6000", "template": "base", "mode": "prototyping",
		"cpu_cores": int64(4), "disk_size_gb": int64(100), "num_gpus": int64(1),
		"http_ports": []int64{}, "allow_snapshot_modify": true, "id": "instance-uuid",
	}
	planValues := cloneInterfaceMap(stateValues)
	planValues["cpu_cores"] = int64(8)
	stateRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), stateValues)
	planRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), planValues)
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s}}
	r.Update(ctx, resource.UpdateRequest{
		Config: tfsdk.Config{Raw: planRaw, Schema: s},
		Plan:   tfsdk.Plan{Raw: planRaw, Schema: s},
		State:  tfsdk.State{Raw: stateRaw, Schema: s},
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("Update() returned no error for unsupported instance version")
	}
	diagnosticText := strings.ToLower(resp.Diagnostics.Errors()[0].Detail())
	if !strings.Contains(diagnosticText, "snapshot") || !strings.Contains(diagnosticText, "manually") || !strings.Contains(diagnosticText, "did not delete") {
		t.Errorf("diagnostic is not actionable/fail-closed: %q", diagnosticText)
	}
	if unexpectedMutations != 0 {
		t.Errorf("unexpected snapshot/delete/create mutations = %d", unexpectedMutations)
	}
}

func TestInstanceCreateRejectsDiskBelowSnapshotMinimumBeforeMutation(t *testing.T) {
	ctx := context.Background()
	createCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/specs", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, gpuSpecsFixture())
	})
	mux.HandleFunc("/v1/thunder-templates", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, map[string]interface{}{})
	})
	mux.HandleFunc("/v1/snapshots/list", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, []map[string]interface{}{{
			"id": "snapshot-id", "name": "snapshot-name", "status": "READY", "minimumDiskSizeGb": 150,
		}})
	})
	mux.HandleFunc("/v1/instances/create", func(w http.ResponseWriter, _ *http.Request) {
		createCalls++
		w.WriteHeader(http.StatusBadRequest)
		writeResourceJSON(t, w, map[string]interface{}{"error": "invalid_request", "message": "disk silently grows"})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	raw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
		"gpu_type": "A6000", "template": "snapshot-name", "mode": "prototyping",
		"cpu_cores": int64(4), "disk_size_gb": int64(100), "num_gpus": int64(1),
		"http_ports": nil, "allow_snapshot_modify": false,
	})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{
		Config: tfsdk.Config{Raw: raw, Schema: s},
		Plan:   tfsdk.Plan{Raw: raw, Schema: s},
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("Create() returned no error for disk below snapshot minimum")
	}
	if createCalls != 0 {
		t.Errorf("instance create calls = %d, want 0", createCalls)
	}
	if !strings.Contains(strings.ToLower(resp.Diagnostics.Errors()[0].Detail()), "150") {
		t.Errorf("diagnostic does not report snapshot minimum: %v", resp.Diagnostics)
	}
}

func TestValidateInstanceConfigurationUsesPublicSpecs(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	registerInstancePreflightFixtures(t, mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}

	tests := []struct {
		name      string
		gpuType   string
		numGPUs   int64
		cpuCores  int64
		diskSize  int64
		wantError bool
	}{
		{name: "valid canonical config", gpuType: "A6000", numGPUs: 1, cpuCores: 4, diskSize: 100},
		{name: "unsupported GPU and count", gpuType: "A6000", numGPUs: 2, cpuCores: 4, diskSize: 100, wantError: true},
		{name: "invalid CPU option", gpuType: "A6000", numGPUs: 1, cpuCores: 6, diskSize: 100, wantError: true},
		{name: "storage below minimum", gpuType: "A6000", numGPUs: 1, cpuCores: 4, diskSize: 49, wantError: true},
		{name: "storage above maximum", gpuType: "A6000", numGPUs: 1, cpuCores: 4, diskSize: 201, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := &InstanceResourceModel{
				GPUType:    types.StringValue(tt.gpuType),
				NumGPUs:    types.Int64Value(tt.numGPUs),
				CPUCores:   types.Int64Value(tt.cpuCores),
				DiskSizeGB: types.Int64Value(tt.diskSize),
				Template:   types.StringValue("base"),
			}
			err := r.validateInstanceConfiguration(ctx, model, nil)
			if (err != nil) != tt.wantError {
				t.Errorf("validateInstanceConfiguration() error = %v, wantError %t", err, tt.wantError)
			}
		})
	}
}

func TestValidateInstanceConfigurationDefersLegacyModeUpdatesToAPI(t *testing.T) {
	ctx := context.Background()
	specCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/specs", func(w http.ResponseWriter, _ *http.Request) {
		specCalls++
		writeResourceJSON(t, w, gpuSpecsFixture())
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}

	tests := []struct {
		name    string
		mode    string
		numGPUs int64
	}{
		{name: "one GPU production", mode: "production", numGPUs: 1},
		{name: "four GPU prototyping", mode: "prototyping", numGPUs: 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := &InstanceResourceModel{
				Mode:       types.StringValue(tt.mode),
				GPUType:    types.StringValue("A6000"),
				NumGPUs:    types.Int64Value(tt.numGPUs),
				CPUCores:   types.Int64Value(4),
				DiskSizeGB: types.Int64Value(100),
			}
			model := &InstanceResourceModel{
				Mode:       types.StringValue(tt.mode),
				GPUType:    types.StringValue("A6000"),
				NumGPUs:    types.Int64Value(tt.numGPUs),
				CPUCores:   types.Int64Value(16),
				DiskSizeGB: types.Int64Value(100),
				Template:   types.StringValue("base"),
			}
			if err := r.validateInstanceConfiguration(ctx, model, previous); err != nil {
				t.Fatalf("validateInstanceConfiguration() rejected a legacy mode update: %v", err)
			}
		})
	}
	if specCalls != 0 {
		t.Errorf("public specs calls = %d, want 0 for legacy off-route updates", specCalls)
	}
}

func TestValidateInstanceConfigurationAllowsUnchangedOversizedDiskOnUpdate(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	registerInstancePreflightFixtures(t, mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	r := &InstanceResource{client: client.NewClient(server.URL, "test-token", "test")}

	previous := &InstanceResourceModel{
		GPUType:    types.StringValue("L40"),
		NumGPUs:    types.Int64Value(1),
		DiskSizeGB: types.Int64Value(250),
	}
	model := &InstanceResourceModel{
		GPUType:    types.StringValue("A6000"),
		NumGPUs:    types.Int64Value(1),
		CPUCores:   types.Int64Value(4),
		DiskSizeGB: types.Int64Value(250),
		Template:   types.StringValue("snapshot-name"),
	}
	if err := r.validateInstanceConfiguration(ctx, model, previous); err != nil {
		t.Fatalf("validateInstanceConfiguration() rejected unchanged oversized disk: %v", err)
	}

	model.DiskSizeGB = types.Int64Value(251)
	if err := r.validateInstanceConfiguration(ctx, model, previous); err == nil {
		t.Fatal("validateInstanceConfiguration() allowed oversized disk growth")
	}
}

func gpuSpecsFixture() map[string]interface{} {
	return map[string]interface{}{
		"specs": map[string]interface{}{
			"a6000_x1": map[string]interface{}{
				"displayName": "NVIDIA RTX A6000", "gpuCount": 1,
				"vcpuOptions": []int{4, 8}, "ramPerVCPUGiB": 4,
				"storageGB": map[string]int{"min": 50, "max": 200}, "vramGB": 48,
			},
		},
	}
}

func registerInstancePreflightFixtures(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	mux.HandleFunc("/v2/specs", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, gpuSpecsFixture())
	})
	mux.HandleFunc("/v1/thunder-templates", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, map[string]interface{}{"base": map[string]interface{}{"displayName": "Base"}})
	})
}

func cloneInterfaceMap(values map[string]interface{}) map[string]interface{} {
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func runningInstanceFixture(httpPorts []int) map[string]interface{} {
	return map[string]interface{}{
		"uuid":          "instance-uuid",
		"name":          "test-instance",
		"status":        "RUNNING",
		"cpuCores":      "4",
		"numGpus":       "1",
		"memory":        "16 GB",
		"storage":       100,
		"gpuType":       "A6000",
		"template":      "base",
		"ip":            "203.0.113.10",
		"port":          2222,
		"httpPorts":     httpPorts,
		"sshPublicKeys": []string{},
		"createdAt":     "2026-07-15T00:00:00Z",
	}
}

func writeResourceJSON(t *testing.T, w http.ResponseWriter, value interface{}) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encoding response: %v", err)
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
