package client

import (
	"context"
	"fmt"
	"net/url"
)

type CreateSnapshotRequest struct {
	InstanceID string `json:"instanceId"`
	Name       string `json:"name"`
}

type CreateSnapshotResponse struct {
	Message string `json:"message"`
}

type Snapshot struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	CreatedAt        int64  `json:"createdAt"`
	MinimumDiskSizeGB int   `json:"minimumDiskSizeGb"`
}

func (c *Client) CreateSnapshot(ctx context.Context, req CreateSnapshotRequest) (*CreateSnapshotResponse, error) {
	var resp CreateSnapshotResponse
	if err := c.doRequest(ctx, "POST", "/snapshots/create", req, &resp); err != nil {
		return nil, fmt.Errorf("creating snapshot: %w", err)
	}
	return &resp, nil
}

func (c *Client) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	var resp []Snapshot
	if err := c.doRequest(ctx, "GET", "/snapshots/list", nil, &resp); err != nil {
		return nil, fmt.Errorf("listing snapshots: %w", err)
	}
	return resp, nil
}

// GetSnapshotByID calls ListSnapshots and returns the first match by ID, or nil.
func (c *Client) GetSnapshotByID(ctx context.Context, id string) (*Snapshot, error) {
	snapshots, err := c.ListSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	for i := range snapshots {
		if snapshots[i].ID == id {
			return &snapshots[i], nil
		}
	}
	return nil, nil
}

// GetSnapshotByName calls ListSnapshots and returns the first match by name, or nil.
func (c *Client) GetSnapshotByName(ctx context.Context, name string) (*Snapshot, error) {
	snapshots, err := c.ListSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	for i := range snapshots {
		if snapshots[i].Name == name {
			return &snapshots[i], nil
		}
	}
	return nil, nil
}

func (c *Client) DeleteSnapshot(ctx context.Context, id string) error {
	if err := c.doRequest(ctx, "DELETE", "/snapshots/"+url.PathEscape(id), nil, nil); err != nil {
		return fmt.Errorf("deleting snapshot %s: %w", id, err)
	}
	return nil
}
