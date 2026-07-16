package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"terraform-provider-thundercompute/internal/client"
)

func TestSnapshotCreateRecoversIdentityAfterAmbiguousResponse(t *testing.T) {
	ctx := context.Background()
	originalPollInterval := snapshotPollIntervalShared
	snapshotPollIntervalShared = time.Millisecond
	defer func() { snapshotPollIntervalShared = originalPollInterval }()
	listCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/snapshots/create", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeResourceJSON(t, w, map[string]interface{}{"error": "internal", "message": "response lost after create"})
	})
	mux.HandleFunc("/v1/snapshots/list", func(w http.ResponseWriter, _ *http.Request) {
		listCalls++
		if listCalls == 1 {
			writeResourceJSON(t, w, []interface{}{})
			return
		}
		status := "CREATING"
		if listCalls > 2 {
			status = "READY"
		}
		writeResourceJSON(t, w, []map[string]interface{}{{
			"id": "snapshot-id", "name": "snapshot-name", "status": status,
			"createdAt": int64(1234), "minimumDiskSizeGb": 100,
		}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &SnapshotResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	raw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
		"instance_id": "instance-uuid", "name": "snapshot-name",
	})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{
		Config: tfsdk.Config{Raw: raw, Schema: s},
		Plan:   tfsdk.Plan{Raw: raw, Schema: s},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create() diagnostics: %v", resp.Diagnostics)
	}
	var gotID, gotStatus types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("id"), &gotID); diags.HasError() {
		t.Fatalf("reading snapshot id: %v", diags)
	}
	if diags := resp.State.GetAttribute(ctx, path.Root("status"), &gotStatus); diags.HasError() {
		t.Fatalf("reading snapshot status: %v", diags)
	}
	if gotID.ValueString() != "snapshot-id" || gotStatus.ValueString() != "READY" {
		t.Errorf("snapshot state = id %q status %q, want snapshot-id/READY", gotID.ValueString(), gotStatus.ValueString())
	}
	if listCalls < 3 {
		t.Errorf("list calls = %d, want identity recovery plus readiness poll", listCalls)
	}
}

func TestSnapshotCreateReturnsRateLimitWithoutRecoveryPolling(t *testing.T) {
	ctx := context.Background()
	listCalls := 0
	createCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/snapshots/list", func(w http.ResponseWriter, _ *http.Request) {
		listCalls++
		if listCalls == 1 {
			writeResourceJSON(t, w, []interface{}{})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		writeResourceJSON(t, w, map[string]interface{}{
			"error":   "not_found",
			"message": "unexpected recovery poll",
		})
	})
	mux.HandleFunc("/v1/snapshots/create", func(w http.ResponseWriter, _ *http.Request) {
		createCalls++
		w.WriteHeader(http.StatusTooManyRequests)
		writeResourceJSON(t, w, map[string]interface{}{
			"error":   "rate_limit_exceeded",
			"message": "Too many requests. Please try again later.",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &SnapshotResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	raw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
		"instance_id": "instance-uuid", "name": "snapshot-name",
	})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{
		Config: tfsdk.Config{Raw: raw, Schema: s},
		Plan:   tfsdk.Plan{Raw: raw, Schema: s},
	}, &resp)

	if !resp.Diagnostics.HasError() {
		t.Fatal("Create() returned no error for a rate-limited snapshot request")
	}
	if resp.Diagnostics.WarningsCount() != 0 {
		t.Errorf("Create() treated a rate limit as ambiguous: %v", resp.Diagnostics)
	}
	if createCalls != 1 {
		t.Errorf("snapshot create calls = %d, want 1", createCalls)
	}
	if listCalls != 1 {
		t.Errorf("snapshot list calls = %d, want only the pre-create uniqueness check", listCalls)
	}
}

func TestSnapshotCreatePersistsFailedSnapshotIdentity(t *testing.T) {
	ctx := context.Background()
	listCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/snapshots/create", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, map[string]interface{}{"message": "creating"})
	})
	mux.HandleFunc("/v1/snapshots/list", func(w http.ResponseWriter, _ *http.Request) {
		listCalls++
		if listCalls == 1 {
			writeResourceJSON(t, w, []interface{}{})
			return
		}
		writeResourceJSON(t, w, []map[string]interface{}{{
			"id": "failed-id", "name": "snapshot-name", "status": "FAILED",
		}})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &SnapshotResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	raw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
		"instance_id": "instance-uuid", "name": "snapshot-name",
	})
	resp := resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{
		Config: tfsdk.Config{Raw: raw, Schema: s},
		Plan:   tfsdk.Plan{Raw: raw, Schema: s},
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("Create() returned no error for failed snapshot")
	}
	var gotID types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("id"), &gotID); diags.HasError() {
		t.Fatalf("reading failed snapshot id: %v", diags)
	}
	if gotID.ValueString() != "failed-id" {
		t.Errorf("failed snapshot id = %q, want failed-id", gotID.ValueString())
	}
}

func TestSnapshotImportCompoundAndLegacyIDs(t *testing.T) {
	ctx := context.Background()
	r := &SnapshotResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema

	tests := []struct {
		name         string
		importID     string
		wantID       string
		wantInstance string
		wantWarning  bool
	}{
		{name: "compound", importID: "snapshot-id,instance-uuid", wantID: "snapshot-id", wantInstance: "instance-uuid"},
		{name: "legacy id only", importID: "snapshot-id", wantID: "snapshot-id", wantWarning: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := resource.ImportStateResponse{State: tfsdk.State{
				Schema: s,
				Raw:    tftypes.NewValue(s.Type().TerraformType(ctx), nil),
			}}
			r.ImportState(ctx, resource.ImportStateRequest{ID: tt.importID}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("ImportState() diagnostics: %v", resp.Diagnostics)
			}
			var gotID, gotInstance types.String
			resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("id"), &gotID)...)
			resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("instance_id"), &gotInstance)...)
			if resp.Diagnostics.HasError() {
				t.Fatalf("reading imported snapshot state: %v", resp.Diagnostics)
			}
			if gotID.ValueString() != tt.wantID || gotInstance.ValueString() != tt.wantInstance {
				t.Errorf("imported id/instance = %q/%q, want %q/%q", gotID.ValueString(), gotInstance.ValueString(), tt.wantID, tt.wantInstance)
			}
			if (resp.Diagnostics.WarningsCount() > 0) != tt.wantWarning {
				t.Errorf("warnings = %v, wantWarning %t", resp.Diagnostics, tt.wantWarning)
			}
		})
	}
}

func TestSnapshotInstanceIDReplacementPlanning(t *testing.T) {
	ctx := context.Background()
	r := &SnapshotResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	terraType := s.Type().TerraformType(ctx)
	nonNullState := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "snapshot-id", "instance_id": "old-instance", "name": "snapshot-name",
	})
	legacyState := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "snapshot-id", "name": "snapshot-name",
	})
	planRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "snapshot-id", "instance_id": "new-instance", "name": "snapshot-name",
	})

	tests := []struct {
		name        string
		stateRaw    tftypes.Value
		stateValue  types.String
		wantReplace bool
	}{
		{name: "legacy import hydration", stateRaw: legacyState, stateValue: types.StringNull()},
		{name: "real instance change", stateRaw: nonNullState, stateValue: types.StringValue("old-instance"), wantReplace: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := planmodifier.StringRequest{
				Plan:       tfsdk.Plan{Raw: planRaw, Schema: s},
				PlanValue:  types.StringValue("new-instance"),
				State:      tfsdk.State{Raw: tt.stateRaw, Schema: s},
				StateValue: tt.stateValue,
			}
			var resp planmodifier.StringResponse
			snapshotInstanceIDRequiresReplace().PlanModifyString(ctx, req, &resp)
			if resp.RequiresReplace != tt.wantReplace {
				t.Errorf("RequiresReplace = %t, want %t", resp.RequiresReplace, tt.wantReplace)
			}
		})
	}
}

func TestSnapshotUpdateHydratesLegacyImportInstanceID(t *testing.T) {
	ctx := context.Background()
	r := &SnapshotResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	terraType := s.Type().TerraformType(ctx)
	stateRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "snapshot-id", "name": "snapshot-name", "status": "READY",
	})
	planRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "snapshot-id", "instance_id": "instance-uuid", "name": "snapshot-name", "status": "READY",
	})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Raw: planRaw, Schema: s},
		State: tfsdk.State{Raw: stateRaw, Schema: s},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update() diagnostics: %v", resp.Diagnostics)
	}
	var gotInstanceID types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("instance_id"), &gotInstanceID); diags.HasError() {
		t.Fatalf("reading hydrated instance_id: %v", diags)
	}
	if gotInstanceID.ValueString() != "instance-uuid" {
		t.Errorf("instance_id = %q, want instance-uuid", gotInstanceID.ValueString())
	}
}

func TestSnapshotUpdateRejectsRealChanges(t *testing.T) {
	ctx := context.Background()
	r := &SnapshotResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	terraType := s.Type().TerraformType(ctx)
	stateRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "snapshot-id", "instance_id": "old-instance", "name": "snapshot-name",
	})
	planRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "snapshot-id", "instance_id": "new-instance", "name": "snapshot-name",
	})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Raw: planRaw, Schema: s},
		State: tfsdk.State{Raw: stateRaw, Schema: s},
	}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("Update() returned no error for a real immutable change")
	}
}

func TestSnapshotReadRemovesDisappearedSnapshot(t *testing.T) {
	ctx := context.Background()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/snapshots/list", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, []interface{}{})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &SnapshotResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	stateRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
		"id": "missing-id", "instance_id": "instance-uuid", "name": "snapshot-name", "status": "READY",
	})
	resp := resource.ReadResponse{State: tfsdk.State{Raw: stateRaw, Schema: s}}
	r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Raw: stateRaw, Schema: s}}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read() diagnostics: %v", resp.Diagnostics)
	}
	if !resp.State.Raw.IsNull() {
		t.Errorf("disappeared snapshot state was not removed: %s", resp.State.Raw)
	}
}
