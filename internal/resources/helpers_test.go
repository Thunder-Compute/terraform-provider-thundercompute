package resources

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"terraform-provider-thundercompute/internal/client"
)

func TestExtractInt64Set_Nil(t *testing.T) {
	result := extractInt64Set(types.SetNull(types.Int64Type))
	if result != nil {
		t.Errorf("expected nil for null set, got %v", result)
	}
}

func TestExtractInt64Set_Unknown(t *testing.T) {
	result := extractInt64Set(types.SetUnknown(types.Int64Type))
	if result != nil {
		t.Errorf("expected nil for unknown set, got %v", result)
	}
}

func TestPortDiff(t *testing.T) {
	tests := []struct {
		name       string
		old, new   []int64
		wantAdd    []int64
		wantRemove []int64
	}{
		{
			name: "no changes",
			old:  []int64{80, 443}, new: []int64{80, 443},
		},
		{
			name: "add only",
			old:  []int64{80}, new: []int64{80, 443},
			wantAdd: []int64{443},
		},
		{
			name: "remove only",
			old:  []int64{80, 443}, new: []int64{80},
			wantRemove: []int64{443},
		},
		{
			name: "both add and remove",
			old:  []int64{80, 443}, new: []int64{80, 8080},
			wantAdd: []int64{8080}, wantRemove: []int64{443},
		},
		{
			name: "empty to ports",
			old:  nil, new: []int64{80, 443},
			wantAdd: []int64{80, 443},
		},
		{
			name: "ports to empty",
			old:  []int64{80, 443}, new: nil,
			wantRemove: []int64{80, 443},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			add, remove := portDiff(tt.old, tt.new)
			if !int64SliceSetEqual(add, tt.wantAdd) {
				t.Errorf("add = %v, want %v", add, tt.wantAdd)
			}
			if !int64SliceSetEqual(remove, tt.wantRemove) {
				t.Errorf("remove = %v, want %v", remove, tt.wantRemove)
			}
		})
	}
}

func TestInt64sToInts(t *testing.T) {
	result := int64sToInts([]int64{1, 2, 3})
	if len(result) != 3 || result[0] != 1 || result[1] != 2 || result[2] != 3 {
		t.Errorf("unexpected result: %v", result)
	}
}

func TestInt64sToInts_Empty(t *testing.T) {
	result := int64sToInts(nil)
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %v", result)
	}
}

func TestParseIntOrZero(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		input string
		want  int64
	}{
		{"42", 42},
		{"0", 0},
		{"", 0},
		{"not-a-number", 0},
		{"-1", -1},
	}
	for _, tt := range tests {
		got := parseIntOrZero(ctx, tt.input)
		if got != tt.want {
			t.Errorf("parseIntOrZero(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestIsModifyDisabled(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "temporarily disabled API error",
			err:  fmt.Errorf("modifying instance 0: %w", &client.APIError{StatusCode: 400, ErrorType: "temporarily_disabled", Message: "Modify is temporarily disabled"}),
			want: true,
		},
		{
			name: "other API error",
			err:  fmt.Errorf("modifying instance 0: %w", &client.APIError{StatusCode: 400, ErrorType: "invalid_request", Message: "bad gpu_type"}),
			want: false,
		},
		{
			name: "non-API error",
			err:  fmt.Errorf("network failure"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "404 not found",
			err:  fmt.Errorf("modifying instance 0: %w", &client.APIError{StatusCode: 404, ErrorType: "not_found"}),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isModifyDisabled(tt.err)
			if got != tt.want {
				t.Errorf("isModifyDisabled(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestInstanceKeyID(t *testing.T) {
	id := instanceKeyID("abc-123", "ssh-ed25519 AAAA... test@test")
	if id == "" {
		t.Error("expected non-empty ID")
	}
	if !strings.HasPrefix(id, "abc-123:") {
		t.Errorf("ID should start with instance UUID, got %q", id)
	}
	// Same inputs produce same ID (deterministic)
	id2 := instanceKeyID("abc-123", "ssh-ed25519 AAAA... test@test")
	if id != id2 {
		t.Errorf("expected deterministic ID, got %q and %q", id, id2)
	}
	// Different key produces different ID
	id3 := instanceKeyID("abc-123", "ssh-rsa AAAA... other@test")
	if id == id3 {
		t.Error("expected different IDs for different keys")
	}
	// Different instance produces different ID
	id4 := instanceKeyID("def-456", "ssh-ed25519 AAAA... test@test")
	if id == id4 {
		t.Error("expected different IDs for different instances")
	}
}

// int64SliceSetEqual compares two int64 slices as sets (order-independent)
func int64SliceSetEqual(a, b []int64) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	set := make(map[int64]bool, len(a))
	for _, v := range a {
		set[v] = true
	}
	for _, v := range b {
		if !set[v] {
			return false
		}
	}
	return true
}
