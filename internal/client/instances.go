package client

import (
	"context"
	"fmt"
	"net/url"
)

// --- Request / Response types (match Thunder Compute OpenAPI spec exactly) ---

type CreateInstanceRequest struct {
	CPUCores   int    `json:"cpu_cores"`
	DiskSizeGB int    `json:"disk_size_gb"`
	GPUType    string `json:"gpu_type"`
	Mode       string `json:"mode"`
	NumGPUs    int    `json:"num_gpus"`
	Template   string `json:"template"`
	PublicKey  string `json:"public_key,omitempty"`
}

type CreateInstanceResponse struct {
	Identifier int    `json:"identifier"`
	Key        string `json:"key"`
	UUID       string `json:"uuid"`
}

type InstanceListItem struct {
	ID               string           `json:"id"`
	UUID             string           `json:"uuid"`
	Name             string           `json:"name"`
	Status           string           `json:"status"`
	CPUCores         string           `json:"cpuCores"` // API returns string
	NumGPUs          string           `json:"numGpus"`  // API returns string
	Memory           string           `json:"memory"`
	Storage          int              `json:"storage"`
	GPUType          string           `json:"gpuType"`
	Mode             string           `json:"mode"`
	Template         string           `json:"template"`
	IP               string           `json:"ip"`
	Port             int              `json:"port"`
	HTTPPorts        []int            `json:"httpPorts"`
	SSHPublicKeys    []string         `json:"sshPublicKeys"`
	CreatedAt        string           `json:"createdAt"`
	ProvisioningTime string           `json:"provisioningTime"`
	RestoringTime    string           `json:"restoringTime"`
	SnapshotSize     int64            `json:"snapshotSize,omitempty"`
	K8s              bool             `json:"k8s"`
	Promoted         bool             `json:"promoted"`
	LastRestart      *InstanceRestart `json:"lastRestart,omitempty"`
}

type InstanceRestart struct {
	ExitCode  int    `json:"exitCode"`
	Message   string `json:"message"`
	Reason    string `json:"reason"`
	Timestamp string `json:"timestamp"`
}

type ModifyInstanceRequest struct {
	CPUCores    *int    `json:"cpu_cores,omitempty"`
	DiskSizeGB  *int    `json:"disk_size_gb,omitempty"`
	GPUType     *string `json:"gpu_type,omitempty"`
	Mode        *string `json:"mode,omitempty"`
	NumGPUs     *int    `json:"num_gpus,omitempty"`
	AddPorts    []int   `json:"add_ports,omitempty"`
	RemovePorts []int   `json:"remove_ports,omitempty"`
}

type ModifyInstanceResponse struct {
	Identifier   string  `json:"identifier"`
	InstanceName string  `json:"instance_name"`
	GPUType      *string `json:"gpu_type,omitempty"`
	Mode         *string `json:"mode,omitempty"`
	NumGPUs      *int    `json:"num_gpus,omitempty"`
	HTTPPorts    []int   `json:"http_ports,omitempty"`
}

type InstanceDeleteResponse struct {
	Message string `json:"message"`
}

type AddKeyToInstanceRequest struct {
	PublicKey string `json:"public_key,omitempty"`
}

type AddKeyToInstanceResponse struct {
	Success bool   `json:"success"`
	UUID    string `json:"uuid"`
	Key     string `json:"key,omitempty"`
	Message string `json:"message,omitempty"`
}

// --- API methods ---

func (c *Client) CreateInstance(ctx context.Context, req CreateInstanceRequest) (*CreateInstanceResponse, error) {
	var resp CreateInstanceResponse
	if err := c.doRequest(ctx, "POST", "/instances/create", req, &resp); err != nil {
		return nil, fmt.Errorf("creating instance: %w", err)
	}
	return &resp, nil
}

func (c *Client) ListInstances(ctx context.Context) (map[string]InstanceListItem, error) {
	var resp map[string]InstanceListItem
	if err := c.doRequest(ctx, "GET", "/instances/list", nil, &resp); err != nil {
		return nil, fmt.Errorf("listing instances: %w", err)
	}
	return resp, nil
}

// GetInstanceByUUID calls ListInstances and returns the matching instance plus its map key (numeric index).
// Returns ("", nil, nil) if no instance matches — callers check the nil item.
func (c *Client) GetInstanceByUUID(ctx context.Context, uuid string) (index string, item *InstanceListItem, err error) {
	instances, err := c.ListInstances(ctx)
	if err != nil {
		return "", nil, err
	}
	for key, inst := range instances {
		if inst.UUID == uuid {
			inst.ID = key // populate from map key per API contract
			return key, &inst, nil
		}
	}
	return "", nil, nil
}

func (c *Client) ModifyInstance(ctx context.Context, id string, req ModifyInstanceRequest) (*ModifyInstanceResponse, error) {
	var resp ModifyInstanceResponse
	if err := c.doRequest(ctx, "POST", "/instances/"+url.PathEscape(id)+"/modify", req, &resp); err != nil {
		return nil, fmt.Errorf("modifying instance %s: %w", id, err)
	}
	return &resp, nil
}

func (c *Client) AddKeyToInstance(ctx context.Context, id string, req AddKeyToInstanceRequest) (*AddKeyToInstanceResponse, error) {
	var resp AddKeyToInstanceResponse
	if err := c.doRequest(ctx, "POST", "/instances/"+url.PathEscape(id)+"/add_key", req, &resp); err != nil {
		return nil, fmt.Errorf("adding key to instance %s: %w", id, err)
	}
	return &resp, nil
}

func (c *Client) DeleteInstance(ctx context.Context, id string) error {
	var resp InstanceDeleteResponse
	if err := c.doRequest(ctx, "POST", "/instances/"+url.PathEscape(id)+"/delete", nil, &resp); err != nil {
		return fmt.Errorf("deleting instance %s: %w", id, err)
	}
	return nil
}
