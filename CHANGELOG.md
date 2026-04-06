# Changelog

## 0.1.0 (Unreleased)

FEATURES:

* **New Resource:** `thundercompute_instance` - Manage Thunder Compute GPU instances with full lifecycle support, including snapshot-based modify fallback
* **New Resource:** `thundercompute_instance_key` - Add SSH public keys to running instances
* **New Resource:** `thundercompute_ssh_key` - Manage organization-level SSH keys
* **New Resource:** `thundercompute_snapshot` - Create and manage instance snapshots
* **New Data Source:** `thundercompute_instances` - List all instances in the organization
* **New Data Source:** `thundercompute_gpu_specs` - Retrieve GPU hardware specifications
* **New Data Source:** `thundercompute_pricing` - Retrieve current hourly pricing
* **New Data Source:** `thundercompute_templates` - List available instance templates

NOTES:

* The Thunder Compute instance modify API is temporarily unavailable. The `thundercompute_instance` resource includes a snapshot-based modify fallback, enabled by setting `allow_snapshot_modify = true`, which snapshots the instance, deletes it, and recreates from the snapshot with updated configuration.
* The Thunder Compute API does not support removing SSH keys from instances. Destroying a `thundercompute_instance_key` resource removes it from Terraform state, but the key remains authorized on the instance.
