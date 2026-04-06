package client

import (
	"context"
	"fmt"
)

// --- GPU Specs ---

type GPUSpecConfig struct {
	DisplayName   string       `json:"displayName"`
	GPUCount      int          `json:"gpuCount"`
	Limits        GPULimits    `json:"limits"`
	Mode          string       `json:"mode"`
	RAMPerVCPUGiB int          `json:"ramPerVCPUGiB"`
	StorageGB     StorageRange `json:"storageGB"`
	VCPUOptions   []int        `json:"vcpuOptions"`
	VRAMGB        int          `json:"vramGB"`
}

type GPULimits struct {
	MaxCPUPerGPU       int `json:"maxCPUPerGPU"`
	MaxMemoryGiBPerGPU int `json:"maxMemoryGiBPerGPU"`
}

type StorageRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// --- Templates ---

type EnvironmentTemplate struct {
	AutomountFolders    []string            `json:"automountFolders,omitempty"`
	CleanupCommands     []string            `json:"cleanupCommands,omitempty"`
	Default             bool                `json:"default,omitempty"`
	DefaultSpecs        *TemplateDefaultSpecs `json:"defaultSpecs,omitempty"`
	DisplayName         string              `json:"displayName"`
	ExtendedDescription string              `json:"extendedDescription,omitempty"`
	OpenPorts           []int               `json:"openPorts,omitempty"`
	StartupCommands     []string            `json:"startupCommands,omitempty"`
	StartupMinutes      int                 `json:"startupMinutes,omitempty"`
	Version             int                 `json:"version,omitempty"`
}

type TemplateDefaultSpecs struct {
	Cores   int    `json:"cores"`
	GPUType string `json:"gpu_type"`
	NumGPUs int    `json:"num_gpus"`
	Storage int    `json:"storage"`
	Template string `json:"template"`
}

// --- API methods ---

func (c *Client) GetPricing(ctx context.Context) (map[string]float64, error) {
	var resp struct {
		Pricing map[string]float64 `json:"pricing"`
	}
	if err := c.doRequest(ctx, "GET", "/pricing", nil, &resp); err != nil {
		return nil, fmt.Errorf("getting pricing: %w", err)
	}
	return resp.Pricing, nil
}

func (c *Client) GetGPUSpecs(ctx context.Context) (map[string]GPUSpecConfig, error) {
	var resp struct {
		Specs map[string]GPUSpecConfig `json:"specs"`
	}
	if err := c.doRequest(ctx, "GET", "/specs", nil, &resp); err != nil {
		return nil, fmt.Errorf("getting gpu specs: %w", err)
	}
	return resp.Specs, nil
}

func (c *Client) GetTemplates(ctx context.Context) (map[string]EnvironmentTemplate, error) {
	var resp map[string]EnvironmentTemplate
	if err := c.doRequest(ctx, "GET", "/thunder-templates", nil, &resp); err != nil {
		return nil, fmt.Errorf("getting templates: %w", err)
	}
	return resp, nil
}
