package resources

import (
	"context"
	"fmt"
	"strings"
	"time"

	"terraform-provider-thundercompute/internal/client"
)

const snapshotPollIntervalShared = 5 * time.Second

// WaitForSnapshot polls until a named snapshot reaches a terminal state.
// Returns the snapshot on success, or an error on failure / context cancellation.
// Used by both SnapshotResource.Create and InstanceResource.updateViaSnapshot.
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
			case "CREATING", "SNAPSHOTTING":
				// still in progress
			case "ERROR", "FAILED":
				return nil, fmt.Errorf("snapshot %q entered error status: %s", name, snap.Status)
			default:
				return snap, nil
			}
		}

		timer.Reset(snapshotPollIntervalShared)
	}
}
