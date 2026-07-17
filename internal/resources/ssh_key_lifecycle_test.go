package resources

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"terraform-provider-thundercompute/internal/client"
)

func TestSSHKeyResourceCRUDAndImport(t *testing.T) {
	ctx := context.Background()
	deleteCalls := 0
	const canonicalPublicKey = "ssh-ed25519 AAAA-valid-key"
	configuredPublicKey := canonicalPublicKey + "\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/keys/add", func(w http.ResponseWriter, req *http.Request) {
		var body client.SSHKeyAddRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Errorf("decoding SSH key request: %v", err)
		}
		if body.PublicKey != canonicalPublicKey {
			t.Errorf("API public key = %q, want normalized %q", body.PublicKey, canonicalPublicKey)
		}
		writeResourceJSON(t, w, map[string]interface{}{"key": map[string]interface{}{
			"id": "key-id", "name": "deploy", "public_key": canonicalPublicKey,
			"fingerprint": "SHA256:fingerprint", "key_type": "ssh-ed25519", "created_at": int64(1234),
		}})
	})
	mux.HandleFunc("/v1/keys/list", func(w http.ResponseWriter, _ *http.Request) {
		writeResourceJSON(t, w, []map[string]interface{}{{
			"id": "key-id", "name": "deploy", "public_key": canonicalPublicKey,
			"fingerprint": "SHA256:fingerprint", "key_type": "ssh-ed25519", "created_at": int64(1234),
		}})
	})
	mux.HandleFunc("/v1/keys/key-id", func(w http.ResponseWriter, req *http.Request) {
		deleteCalls++
		if req.Method != http.MethodDelete {
			t.Errorf("delete method = %s, want DELETE", req.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	r := &SSHKeyResource{client: client.NewClient(server.URL, "test-token", "test")}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	planRaw := instancePlanningValue(ctx, t, s.Type().TerraformType(ctx), map[string]interface{}{
		"name": "deploy", "public_key": configuredPublicKey,
	})
	createResp := resource.CreateResponse{State: tfsdk.State{Schema: s}}
	r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Raw: planRaw, Schema: s}}, &createResp)
	if createResp.Diagnostics.HasError() {
		t.Fatalf("Create() diagnostics: %v", createResp.Diagnostics)
	}

	readResp := resource.ReadResponse{State: tfsdk.State{Raw: createResp.State.Raw, Schema: s}}
	r.Read(ctx, resource.ReadRequest{State: createResp.State}, &readResp)
	if readResp.Diagnostics.HasError() {
		t.Fatalf("Read() diagnostics: %v", readResp.Diagnostics)
	}
	var gotID, gotPublicKey types.String
	if diags := readResp.State.GetAttribute(ctx, path.Root("id"), &gotID); diags.HasError() {
		t.Fatalf("reading key id: %v", diags)
	}
	if gotID.ValueString() != "key-id" {
		t.Errorf("key id = %q, want key-id", gotID.ValueString())
	}
	if diags := readResp.State.GetAttribute(ctx, path.Root("public_key"), &gotPublicKey); diags.HasError() {
		t.Fatalf("reading public key: %v", diags)
	}
	if gotPublicKey.ValueString() != configuredPublicKey {
		t.Errorf("state public key = %q, want configured value %q", gotPublicKey.ValueString(), configuredPublicKey)
	}

	var deleteResp resource.DeleteResponse
	r.Delete(ctx, resource.DeleteRequest{State: readResp.State}, &deleteResp)
	if deleteResp.Diagnostics.HasError() {
		t.Fatalf("Delete() diagnostics: %v", deleteResp.Diagnostics)
	}
	if deleteCalls != 1 {
		t.Errorf("delete calls = %d, want 1", deleteCalls)
	}

	importResp := resource.ImportStateResponse{State: tfsdk.State{
		Schema: s,
		Raw:    tftypes.NewValue(s.Type().TerraformType(ctx), nil),
	}}
	r.ImportState(ctx, resource.ImportStateRequest{ID: "imported-key-id"}, &importResp)
	if importResp.Diagnostics.HasError() {
		t.Fatalf("ImportState() diagnostics: %v", importResp.Diagnostics)
	}
	if diags := importResp.State.GetAttribute(ctx, path.Root("id"), &gotID); diags.HasError() {
		t.Fatalf("reading imported key id: %v", diags)
	}
	if gotID.ValueString() != "imported-key-id" {
		t.Errorf("imported key id = %q, want imported-key-id", gotID.ValueString())
	}
}

func TestSSHKeyPublicKeyWhitespaceDoesNotReplace(t *testing.T) {
	ctx := context.Background()
	r := &SSHKeyResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	terraType := s.Type().TerraformType(ctx)
	tests := []struct {
		name        string
		state       string
		plan        string
		wantReplace bool
	}{
		{name: "trailing newline", state: "ssh-ed25519 AAAA-valid-key", plan: "ssh-ed25519 AAAA-valid-key\n"},
		{name: "different key", state: "ssh-ed25519 AAAA-old-key", plan: "ssh-ed25519 AAAA-new-key", wantReplace: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
				"name": "deploy", "public_key": tt.state,
			})
			planRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
				"name": "deploy", "public_key": tt.plan,
			})
			req := planmodifier.StringRequest{
				State:      tfsdk.State{Raw: stateRaw, Schema: s},
				StateValue: types.StringValue(tt.state),
				Plan:       tfsdk.Plan{Raw: planRaw, Schema: s},
				PlanValue:  types.StringValue(tt.plan),
			}
			var resp planmodifier.StringResponse
			sshKeyPublicKeyRequiresReplace().PlanModifyString(ctx, req, &resp)
			if resp.RequiresReplace != tt.wantReplace {
				t.Errorf("RequiresReplace = %t, want %t", resp.RequiresReplace, tt.wantReplace)
			}
		})
	}
}

func TestSSHKeyUpdateAdoptsEquivalentConfiguredWhitespace(t *testing.T) {
	ctx := context.Background()
	r := &SSHKeyResource{}
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	terraType := s.Type().TerraformType(ctx)
	stateRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "key-id", "name": "deploy", "public_key": "ssh-ed25519 AAAA-valid-key",
	})
	planRaw := instancePlanningValue(ctx, t, terraType, map[string]interface{}{
		"id": "key-id", "name": "deploy", "public_key": "ssh-ed25519 AAAA-valid-key\n",
	})
	resp := resource.UpdateResponse{State: tfsdk.State{Schema: s}}
	r.Update(ctx, resource.UpdateRequest{
		Plan:  tfsdk.Plan{Raw: planRaw, Schema: s},
		State: tfsdk.State{Raw: stateRaw, Schema: s},
	}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update() diagnostics: %v", resp.Diagnostics)
	}
	var gotPublicKey types.String
	if diags := resp.State.GetAttribute(ctx, path.Root("public_key"), &gotPublicKey); diags.HasError() {
		t.Fatalf("reading public key: %v", diags)
	}
	if gotPublicKey.ValueString() != "ssh-ed25519 AAAA-valid-key\n" {
		t.Errorf("state public key = %q, want configured trailing newline", gotPublicKey.ValueString())
	}
}
