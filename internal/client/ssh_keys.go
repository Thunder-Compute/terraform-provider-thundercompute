package client

import (
	"context"
	"fmt"
	"net/url"
)

type SSHKeyAddRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"public_key"`
}

type SSHKeyAddResponse struct {
	Key     *SSHKey `json:"key,omitempty"`
	Message string  `json:"message,omitempty"`
}

type SSHKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint,omitempty"`
	KeyType     string `json:"key_type,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
}

func (c *Client) AddSSHKey(ctx context.Context, req SSHKeyAddRequest) (*SSHKeyAddResponse, error) {
	var resp SSHKeyAddResponse
	if err := c.doRequest(ctx, "POST", "/keys/add", req, &resp); err != nil {
		return nil, fmt.Errorf("adding ssh key: %w", err)
	}
	return &resp, nil
}

func (c *Client) ListSSHKeys(ctx context.Context) ([]SSHKey, error) {
	var resp []SSHKey
	if err := c.doRequest(ctx, "GET", "/keys/list", nil, &resp); err != nil {
		return nil, fmt.Errorf("listing ssh keys: %w", err)
	}
	return resp, nil
}

// GetSSHKeyByID calls ListSSHKeys and returns the first match by ID, or nil.
func (c *Client) GetSSHKeyByID(ctx context.Context, id string) (*SSHKey, error) {
	keys, err := c.ListSSHKeys(ctx)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == id {
			return &keys[i], nil
		}
	}
	return nil, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	if err := c.doRequest(ctx, "DELETE", "/keys/"+url.PathEscape(id), nil, nil); err != nil {
		return fmt.Errorf("deleting ssh key %s: %w", id, err)
	}
	return nil
}
