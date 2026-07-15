package resources

import (
	"context"
	"fmt"
	"strings"
	"time"

	"terraform-provider-thundercompute/internal/client"
)

var snapshotPollIntervalShared = 5 * time.Second

// WaitForSnapshot polls until a named snapshot reaches a terminal state.
// Returns the snapshot on success, or an error on failure / context cancellation.
func WaitForSnapshot(ctx context.Context, c *client.Client, name string) (*client.Snapshot, error) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for snapshot %q: %w", name, ctx.Err())
		case <-timer.C:
		}

		snap, err := c.GetSnapshotByName(ctx, name)
		if err != nil {
			if client.IsPermanentError(err) {
				return nil, fmt.Errorf("permanent error waiting for snapshot %q: %w", name, err)
			}
			timer.Reset(snapshotPollIntervalShared)
			continue
		}

		if snap != nil {
			switch strings.ToUpper(snap.Status) {
			case "CREATING":
				// still in progress
			case "FAILED", "ERROR", "UNKNOWN":
				return nil, fmt.Errorf("snapshot %q entered error status: %s", name, snap.Status)
			case "READY":
				return snap, nil
			default:
				return nil, fmt.Errorf("snapshot %q returned unrecognized status: %s", name, snap.Status)
			}
		}

		timer.Reset(snapshotPollIntervalShared)
	}
}

// WaitForSnapshotVisible returns as soon as the create result has an API
// identity, even while the snapshot is still being built.
func WaitForSnapshotVisible(ctx context.Context, c *client.Client, name string) (*client.Snapshot, error) {
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for snapshot %q identity: %w", name, ctx.Err())
		case <-timer.C:
		}

		snap, err := c.GetSnapshotByName(ctx, name)
		if err != nil {
			if client.IsPermanentError(err) {
				return nil, fmt.Errorf("permanent error recovering snapshot %q identity: %w", name, err)
			}
			timer.Reset(snapshotPollIntervalShared)
			continue
		}
		if snap != nil {
			return snap, nil
		}
		timer.Reset(snapshotPollIntervalShared)
	}
}
