package resources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
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

func TestSnapshotCreateBoundsAmbiguousIdentityRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	originalPollInterval := snapshotPollIntervalShared
	originalRecoveryTimeout := snapshotAmbiguousCreateRecoveryTimeout
	snapshotPollIntervalShared = time.Millisecond
	snapshotAmbiguousCreateRecoveryTimeout = 10 * time.Millisecond
	defer func() {
		snapshotPollIntervalShared = originalPollInterval
		snapshotAmbiguousCreateRecoveryTimeout = originalRecoveryTimeout
	}()

	listCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/snapshots/create", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeResourceJSON(t, w, map[string]interface{}{"error": "internal", "message": "response lost after create"})
	})
	mux.HandleFunc("/v1/snapshots/list", func(w http.ResponseWriter, _ *http.Request) {
		listCalls++
		writeResourceJSON(t, w, []interface{}{})
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
		t.Fatal("Create() returned no error after ambiguous identity recovery expired")
	}
	if ctx.Err() != nil {
		t.Fatal("ambiguous identity recovery consumed the entire caller/create timeout")
	}
	if listCalls < 2 {
		t.Errorf("snapshot list calls = %d, want a uniqueness check and bounded recovery polling", listCalls)
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

func TestSnapshotCreatePersistsReturnedIDWhenListNeverVisible(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	originalPollInterval := snapshotPollIntervalShared
	snapshotPollIntervalShared = time.Millisecond
	defer func() { snapshotPollIntervalShared = originalPollInterval }()

	createCalls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/snapshots/create", func(w http.ResponseWriter, _ *http.Request) {
		createCalls++
		writeResourceJSON(t, w, map[string]interface{}{"id": "returned-id", "message": "Snapshot created"})
	})
	// The snapshot list stays empty forever: it satisfies the pre-create
	// uniqueness check but never surfaces the created snapshot, modeling stale
	// or failing list consistency after a successful create.
	mux.HandleFunc("/v1/snapshots/list", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, []interface{}{})
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

	// Readiness cannot be confirmed from an unavailable list, so Create fails...
	if !resp.Diagnostics.HasError() {
		t.Fatal("Create() returned no error when the snapshot list never became visible")
	}
	if createCalls != 1 {
		t.Errorf("snapshot create calls = %d, want 1", createCalls)
	}
	// ...but the server-returned ID must be persisted so the snapshot is
	// recoverable (deletable/reconcilable) instead of orphaned.
	var gotID types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("id"), &gotID); diags.HasError() {
		t.Fatalf("reading snapshot id: %v", diags)
	}
	if gotID.ValueString() != "returned-id" {
		t.Errorf("snapshot id = %q, want returned-id (server-returned identity must survive a stale list)", gotID.ValueString())
	}
}

func TestSnapshotImportRequiresCompoundID(t *testing.T) {
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
		wantError    bool
	}{
		{name: "compound", importID: "snapshot-id,instance-uuid", wantID: "snapshot-id", wantInstance: "instance-uuid"},
		{name: "id only", importID: "snapshot-id", wantError: true},
		{name: "empty snapshot id", importID: ",instance-uuid", wantError: true},
		{name: "empty instance id", importID: "snapshot-id,", wantError: true},
		{name: "extra compound field", importID: "snapshot-id,instance-uuid,extra", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := resource.ImportStateResponse{State: tfsdk.State{
				Schema: s,
				Raw:    tftypes.NewValue(s.Type().TerraformType(ctx), nil),
			}}
			r.ImportState(ctx, resource.ImportStateRequest{ID: tt.importID}, &resp)
			if resp.Diagnostics.HasError() != tt.wantError {
				t.Fatalf("ImportState() diagnostics = %v, wantError %t", resp.Diagnostics, tt.wantError)
			}
			if tt.wantError {
				return
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
			if resp.Diagnostics.WarningsCount() != 0 {
				t.Errorf("warnings = %v, want none", resp.Diagnostics)
			}
		})
	}
}

func TestSnapshotUpdateAcceptsTimeoutChanges(t *testing.T) {
	ctx := context.Background()
	r := &SnapshotResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	terraType := s.Type().TerraformType(ctx)
	stateRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "snapshot-id", "instance_id": "instance-uuid", "name": "snapshot-name", "status": "READY",
		"timeouts": snapshotTimeoutsTerraformValue("10m", "5m"),
	})
	planRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "snapshot-id", "instance_id": "instance-uuid", "name": "snapshot-name", "status": "READY",
		"timeouts": snapshotTimeoutsTerraformValue("20m", "7m"),
	})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Raw: planRaw, Schema: s},
		State: tfsdk.State{Raw: stateRaw, Schema: s},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update() diagnostics: %v", resp.Diagnostics)
	}

	var gotTimeouts timeouts.Value
	if diags := resp.State.GetAttribute(ctx, path.Root("timeouts"), &gotTimeouts); diags.HasError() {
		t.Fatalf("reading snapshot timeouts: %v", diags)
	}
	gotCreate, diags := gotTimeouts.Create(ctx, 0)
	if diags.HasError() {
		t.Fatalf("reading create timeout: %v", diags)
	}
	gotDelete, diags := gotTimeouts.Delete(ctx, 0)
	if diags.HasError() {
		t.Fatalf("reading delete timeout: %v", diags)
	}
	if gotCreate != 20*time.Minute || gotDelete != 7*time.Minute {
		t.Errorf("timeouts = create %s/delete %s, want 20m/7m", gotCreate, gotDelete)
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

func snapshotTimeoutsTerraformValue(create, delete string) map[string]tftypes.Value {
	return map[string]tftypes.Value{
		"create": tftypes.NewValue(tftypes.String, create),
		"delete": tftypes.NewValue(tftypes.String, delete),
	}
}
