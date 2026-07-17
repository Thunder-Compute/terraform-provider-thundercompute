# Changelog

## 0.2.0 (Unreleased)

FEATURES:

* Thunder Compute instances no longer have a `mode`; the provider now uses the current mode-less create and update APIs.
* Instance updates use the current modify and port APIs without destructive snapshot fallback.
* Data sources use the current versioned API routes and expose canonical GPU availability, specification, and pricing data.
* Legacy provider API URLs ending in `/v1` or `/v2` are normalized to the API root.

BUG FIXES:

* Structured API errors are preserved after safe GET and PATCH retries are exhausted.
* Legacy snapshot imports can hydrate `instance_id` from configuration without replacing the imported snapshot.
* In-place instance updates preserve the instance UUID so dependent snapshots and SSH keys are not replaced.
* `public_key` accepts newline-terminated OpenSSH public key file content (for example from `file(...)`) while rejecting embedded multiline data.
* Instance create and modify waits no longer treat the transient `UNKNOWN` status as terminal; the API reports `UNKNOWN` while it cannot determine instance state (for example during early provisioning), so polling continues until the operation timeout.

DEPRECATIONS:

* `mode` and `allow_snapshot_modify` remain in schema for v0.1.0 state compatibility but should be removed from configuration.

## 0.1.0

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

* Version 0.1.0 shipped while the instance modify API was unavailable and included an opt-in snapshot-based modify fallback. Version 0.2.0 removes that destructive fallback now that the modify API is available.
* The Thunder Compute API does not support removing SSH keys from instances. Destroying a `thundercompute_instance_key` resource removes it from Terraform state, but the key remains authorized on the instance.
