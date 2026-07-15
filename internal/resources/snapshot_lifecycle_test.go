package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
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
