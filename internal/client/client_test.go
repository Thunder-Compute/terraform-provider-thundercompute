package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestServer(handler http.Handler) (*httptest.Server, *Client) {
	srv := httptest.NewServer(handler)
	c := NewClient(srv.URL, "test-token", "test")
	c.httpClient = srv.Client()
	return srv, c
}

// --- doRequest tests ---

func TestDoRequest_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header = %q, want Bearer test-token", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("accept header = %q, want application/json", got)
		}
		if got := r.Header.Get("User-Agent"); got != "terraform-provider-thundercompute/test" {
			t.Errorf("user-agent header = %q, want terraform-provider-thundercompute/test", got)
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	var result map[string]string
	err := c.doRequest(context.Background(), "GET", "/test", nil, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["status"] != "ok" {
		t.Errorf("status = %q, want ok", result["status"])
	}
}

func TestDoRequest_PostWithBody(t *testing.T) {
	var gotBody map[string]interface{}

	mux := http.NewServeMux()
	mux.HandleFunc("/post", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	body := map[string]string{"key": "value"}
	err := c.doRequest(context.Background(), "POST", "/post", body, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["key"] != "value" {
		t.Errorf("body key = %v, want value", gotBody["key"])
	}
}

func TestDoRequest_NoAuthWhenTokenEmpty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/noauth", func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no auth header, got %q", auth)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := NewClient(srv.URL, "", "test")
	c.httpClient = srv.Client()

	err := c.doRequest(context.Background(), "GET", "/noauth", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoRequest_ContextCancellation(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.doRequest(ctx, "GET", "/slow", nil, nil)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestDoRequest_EmptyResponseBody(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	var result map[string]string
	err := c.doRequest(context.Background(), "GET", "/empty", nil, &result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// result should remain nil/empty — no unmarshal error on empty body
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestDoRequest_LargeResponseBounded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/large", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write 11MB — exceeds the 10MB limit
		data := make([]byte, 11<<20)
		for i := range data {
			data[i] = 'x'
		}
		w.Write(data)
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	var result map[string]string
	err := c.doRequest(context.Background(), "GET", "/large", nil, &result)
	// Should fail to unmarshal truncated response, not OOM
	if err == nil {
		t.Fatal("expected error from oversized response")
	}
}

// --- Error classification tests ---

func TestDoRequest_APIError(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		body          APIError
		wantNotFound  bool
		wantConflict  bool
		wantPermanent bool
	}{
		{
			name:       "400 bad request",
			statusCode: 400,
			body:       APIError{StatusCode: 400, ErrorType: "invalid_request", Message: "bad gpu_type"},
		},
		{
			name:          "401 unauthorized",
			statusCode:    401,
			body:          APIError{StatusCode: 401, ErrorType: "unauthorized", Message: "bad token"},
			wantPermanent: true,
		},
		{
			name:          "403 forbidden",
			statusCode:    403,
			body:          APIError{StatusCode: 403, ErrorType: "forbidden", Message: "no access"},
			wantPermanent: true,
		},
		{
			name:          "404 not found",
			statusCode:    404,
			body:          APIError{StatusCode: 404, ErrorType: "not_found", Message: "instance not found"},
			wantNotFound:  true,
			wantPermanent: true,
		},
		{
			name:         "409 conflict",
			statusCode:   409,
			body:         APIError{StatusCode: 409, ErrorType: "conflict", Message: "key already exists"},
			wantConflict: true,
		},
		{
			name:       "500 internal",
			statusCode: 500,
			body:       APIError{StatusCode: 500, ErrorType: "internal_error", Message: "unexpected"},
		},
		{
			name:       "502 bad gateway",
			statusCode: 502,
			body:       APIError{StatusCode: 502, ErrorType: "bad_gateway", Message: "upstream"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				json.NewEncoder(w).Encode(tt.body)
			})

			srv, c := newTestServer(mux)
			defer srv.Close()

			err := c.doRequest(context.Background(), "GET", "/fail", nil, nil)
			if err == nil {
				t.Fatal("expected error, got nil")
			}

			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("expected *APIError, got %T", err)
			}
			if apiErr.StatusCode != tt.statusCode {
				t.Errorf("status = %d, want %d", apiErr.StatusCode, tt.statusCode)
			}
			if IsNotFoundError(err) != tt.wantNotFound {
				t.Errorf("IsNotFound = %v, want %v", IsNotFoundError(err), tt.wantNotFound)
			}
			if IsConflictError(err) != tt.wantConflict {
				t.Errorf("IsConflict = %v, want %v", IsConflictError(err), tt.wantConflict)
			}
			if IsPermanentError(err) != tt.wantPermanent {
				t.Errorf("IsPermanent = %v, want %v", IsPermanentError(err), tt.wantPermanent)
			}
		})
	}
}

func TestDoRequest_UnparsableError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("not json"))
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	err := c.doRequest(context.Background(), "GET", "/broken", nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 502 {
		t.Errorf("status = %d, want 502", apiErr.StatusCode)
	}
	if apiErr.ErrorType != "Bad Gateway" {
		t.Errorf("error_type = %q, want 'Bad Gateway'", apiErr.ErrorType)
	}
}

func TestErrorHelpers_NonAPIError(t *testing.T) {
	err := io.EOF
	if IsNotFoundError(err) {
		t.Error("IsNotFoundError should be false for non-APIError")
	}
	if IsConflictError(err) {
		t.Error("IsConflictError should be false for non-APIError")
	}
	if IsPermanentError(err) {
		t.Error("IsPermanentError should be false for non-APIError")
	}
}

// --- Instance API tests ---

func TestGetInstanceByUUID(t *testing.T) {
	instances := map[string]InstanceListItem{
		"0": {UUID: "aaa-111", Name: "first", Status: "RUNNING", GPUType: "H100"},
		"1": {UUID: "bbb-222", Name: "second", Status: "RUNNING", GPUType: "A100"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/instances/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(instances)
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	idx, item, err := c.GetInstanceByUUID(context.Background(), "bbb-222")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if idx != "1" {
		t.Errorf("index = %q, want 1", idx)
	}
	if item.Name != "second" {
		t.Errorf("name = %q, want second", item.Name)
	}
	if item.ID != "1" {
		t.Errorf("id should be populated from map key, got %q", item.ID)
	}

	_, item, err = c.GetInstanceByUUID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item != nil {
		t.Errorf("expected nil item, got %+v", item)
	}
}

func TestGetInstanceByUUID_EmptyList(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/instances/list", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{}"))
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	_, item, err := c.GetInstanceByUUID(context.Background(), "any")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if item != nil {
		t.Error("expected nil for empty instance list")
	}
}

func TestCreateInstance_RequestBody(t *testing.T) {
	var gotReq CreateInstanceRequest

	mux := http.NewServeMux()
	mux.HandleFunc("/instances/create", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		json.NewDecoder(r.Body).Decode(&gotReq)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(CreateInstanceResponse{Identifier: 0, UUID: "new-uuid", Key: "privkey"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	req := CreateInstanceRequest{
		CPUCores: 4, DiskSizeGB: 100, GPUType: "H100",
		Mode: "prototyping", NumGPUs: 1, Template: "ubuntu-22.04",
	}
	resp, err := c.CreateInstance(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.GPUType != "H100" {
		t.Errorf("gpu_type = %q, want H100", gotReq.GPUType)
	}
	if resp.UUID != "new-uuid" {
		t.Errorf("uuid = %q, want new-uuid", resp.UUID)
	}
	if resp.Key != "privkey" {
		t.Errorf("key = %q, want privkey", resp.Key)
	}
}

func TestDeleteInstance_PathEscaping(t *testing.T) {
	var gotPath string

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"Success"}`))
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	// Verify that path params are properly escaped
	_ = c.DeleteInstance(context.Background(), "safe-id-123")
	if !strings.Contains(gotPath, "safe-id-123") {
		t.Errorf("path = %q, want to contain safe-id-123", gotPath)
	}
}

func TestAddKeyToInstance_WithPublicKey(t *testing.T) {
	var gotBody map[string]interface{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/add_key") {
			t.Errorf("path = %q, want to contain /add_key", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(AddKeyToInstanceResponse{
			Success: true,
			UUID:    "inst-uuid",
			Message: "Key added",
		})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	resp, err := c.AddKeyToInstance(context.Background(), "0", AddKeyToInstanceRequest{
		PublicKey: "ssh-ed25519 AAAA... test@test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.UUID != "inst-uuid" {
		t.Errorf("uuid = %q, want inst-uuid", resp.UUID)
	}
	if gotBody["public_key"] != "ssh-ed25519 AAAA... test@test" {
		t.Errorf("public_key = %v, want ssh-ed25519 AAAA... test@test", gotBody["public_key"])
	}
}

func TestAddKeyToInstance_GenerateKey(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(AddKeyToInstanceResponse{
			Success: true,
			UUID:    "inst-uuid",
			Key:     "-----BEGIN OPENSSH PRIVATE KEY-----\ngenerated\n-----END OPENSSH PRIVATE KEY-----",
		})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	resp, err := c.AddKeyToInstance(context.Background(), "0", AddKeyToInstanceRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if resp.Key == "" {
		t.Error("expected generated private key in response")
	}
}

func TestAddKeyToInstance_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(APIError{StatusCode: 404, ErrorType: "not_found", Message: "instance not found"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	_, err := c.AddKeyToInstance(context.Background(), "999", AddKeyToInstanceRequest{
		PublicKey: "ssh-ed25519 AAAA...",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFoundError(err) {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestModifyInstance_PartialBody(t *testing.T) {
	var gotBody map[string]interface{}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ModifyInstanceResponse{Identifier: "0", InstanceName: "test"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	cores := 8
	req := ModifyInstanceRequest{CPUCores: &cores}
	_, err := c.ModifyInstance(context.Background(), "0", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody["cpu_cores"] != float64(8) {
		t.Errorf("cpu_cores = %v, want 8", gotBody["cpu_cores"])
	}
	// Omitted fields should not be present
	if _, exists := gotBody["gpu_type"]; exists {
		t.Error("gpu_type should be omitted from partial modify request")
	}
}

// --- Snapshot API tests ---

func TestGetSnapshotByName(t *testing.T) {
	snapshots := []Snapshot{
		{ID: "snap-1", Name: "checkpoint-v1", Status: "ready", CreatedAt: 1000},
		{ID: "snap-2", Name: "checkpoint-v2", Status: "creating", CreatedAt: 2000},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/snapshots/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(snapshots)
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	snap, err := c.GetSnapshotByName(context.Background(), "checkpoint-v2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.ID != "snap-2" {
		t.Errorf("id = %q, want snap-2", snap.ID)
	}

	snap, err = c.GetSnapshotByName(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap != nil {
		t.Error("expected nil for nonexistent snapshot name")
	}
}

func TestGetSnapshotByID(t *testing.T) {
	snapshots := []Snapshot{
		{ID: "snap-1", Name: "a", Status: "ready"},
		{ID: "snap-2", Name: "b", Status: "ready"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/snapshots/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(snapshots)
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	snap, err := c.GetSnapshotByID(context.Background(), "snap-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Name != "a" {
		t.Errorf("name = %q, want a", snap.Name)
	}

	snap, err = c.GetSnapshotByID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap != nil {
		t.Error("expected nil for nonexistent snapshot id")
	}
}

func TestCreateSnapshot_202Accepted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/snapshots/create", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(CreateSnapshotResponse{Message: "Snapshot creation started"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	resp, err := c.CreateSnapshot(context.Background(), CreateSnapshotRequest{
		InstanceID: "inst-1", Name: "test-snap",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Message == "" {
		t.Error("expected non-empty message")
	}
}

// --- SSH Key API tests ---

func TestGetSSHKeyByID(t *testing.T) {
	keys := []SSHKey{
		{ID: "key-1", Name: "deploy", Fingerprint: "SHA256:abc"},
		{ID: "key-2", Name: "ci", Fingerprint: "SHA256:def"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/keys/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(keys)
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	key, err := c.GetSSHKeyByID(context.Background(), "key-2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.Name != "ci" {
		t.Errorf("name = %q, want ci", key.Name)
	}

	key, err = c.GetSSHKeyByID(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != nil {
		t.Error("expected nil for nonexistent key id")
	}
}

func TestAddSSHKey_ConflictError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/keys/add", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(APIError{StatusCode: 409, ErrorType: "conflict", Message: "key exists"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	_, err := c.AddSSHKey(context.Background(), SSHKeyAddRequest{Name: "dup", PublicKey: "ssh-ed25519 AAAA..."})
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsConflictError(err) {
		t.Errorf("expected conflict error, got: %v", err)
	}
}

// --- Utilities API tests ---

func TestGetPricing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/pricing", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"pricing": map[string]float64{"h100_x1_prototyping": 1.38, "a6000_x1_prototyping": 0.35},
		})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	pricing, err := c.GetPricing(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pricing["a6000_x1_prototyping"] != 0.35 {
		t.Errorf("a6000 price = %f, want 0.35", pricing["a6000_x1_prototyping"])
	}
}

func TestGetGPUSpecs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/specs", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"specs": map[string]GPUSpecConfig{
				"a6000_x1_prototyping": {DisplayName: "A6000", VRAMGB: 48, GPUCount: 1},
			},
		})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	specs, err := c.GetGPUSpecs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if specs["a6000_x1_prototyping"].VRAMGB != 48 {
		t.Errorf("vram = %d, want 48", specs["a6000_x1_prototyping"].VRAMGB)
	}
}

func TestGetTemplates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/thunder-templates", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]EnvironmentTemplate{
			"base": {DisplayName: "Base Image", Default: true},
		})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	tmpls, err := c.GetTemplates(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tmpls["base"].DisplayName != "Base Image" {
		t.Errorf("display_name = %q, want 'Base Image'", tmpls["base"].DisplayName)
	}
}

// --- NewClient defaults ---

func TestNewClient_DefaultBaseURL(t *testing.T) {
	c := NewClient("", "tok", "")
	if c.baseURL != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, defaultBaseURL)
	}
	if c.userAgent != "terraform-provider-thundercompute/dev" {
		t.Errorf("userAgent = %q, want dev default", c.userAgent)
	}
}

func TestNewClient_CustomBaseURL(t *testing.T) {
	c := NewClient("https://custom:9999/v2", "tok", "1.0.0")
	if c.baseURL != "https://custom:9999/v2" {
		t.Errorf("baseURL = %q, want custom", c.baseURL)
	}
	if c.userAgent != "terraform-provider-thundercompute/1.0.0" {
		t.Errorf("userAgent = %q, want version 1.0.0", c.userAgent)
	}
}

// --- Delete / 404 handling tests ---

func TestDeleteSSHKey_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"Success"}`))
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	err := c.DeleteSSHKey(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteSSHKey_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(APIError{StatusCode: 404, ErrorType: "not_found", Message: "key not found"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	err := c.DeleteSSHKey(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFoundError(err) {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestDeleteSnapshot_Success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message":"Snapshot deleted successfully"}`))
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	err := c.DeleteSnapshot(context.Background(), "snap-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeleteSnapshot_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(APIError{StatusCode: 404, ErrorType: "not_found"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	err := c.DeleteSnapshot(context.Background(), "nonexistent")
	if !IsNotFoundError(err) {
		t.Errorf("expected not found error, got: %v", err)
	}
}

func TestListSSHKeys_Empty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/keys/list", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	keys, err := c.ListSSHKeys(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected empty list, got %d keys", len(keys))
	}
}

func TestListSnapshots_Empty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/snapshots/list", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("[]"))
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	snaps, err := c.ListSnapshots(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snaps) != 0 {
		t.Errorf("expected empty list, got %d snapshots", len(snaps))
	}
}

func TestModifyInstance_TemporarilyDisabled(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{StatusCode: 400, ErrorType: "temporarily_disabled", Message: "Modify is temporarily disabled"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	cores := 8
	_, err := c.ModifyInstance(context.Background(), "0", ModifyInstanceRequest{CPUCores: &cores})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.ErrorType != "temporarily_disabled" {
		t.Errorf("error_type = %q, want temporarily_disabled", apiErr.ErrorType)
	}
}

func TestListInstances_Success(t *testing.T) {
	instances := map[string]InstanceListItem{
		"0": {UUID: "aaa-111", Name: "inst-0", Status: "RUNNING"},
		"1": {UUID: "bbb-222", Name: "inst-1", Status: "STOPPED"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/instances/list", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(instances)
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	result, err := c.ListInstances(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 instances, got %d", len(result))
	}
}

func TestCreateInstance_ErrorResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/instances/create", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(APIError{StatusCode: 400, ErrorType: "invalid_request", Message: "bad gpu_type"})
	})

	srv, c := newTestServer(mux)
	defer srv.Close()

	_, err := c.CreateInstance(context.Background(), CreateInstanceRequest{GPUType: "INVALID"})
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("status = %d, want 400", apiErr.StatusCode)
	}
}
