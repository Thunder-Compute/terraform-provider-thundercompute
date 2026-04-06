package resources

import (
	"context"
	"testing"
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
		"mode": true, "num_gpus": true, "http_ports": true,
	}
	for _, attr := range configTriggerAttributes {
		if !expected[attr] {
			t.Errorf("unexpected trigger attribute: %q", attr)
		}
	}
}
