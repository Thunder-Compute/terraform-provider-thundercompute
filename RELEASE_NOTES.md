# Thunder Compute Terraform Provider v0.2.0

v0.2.0 aligns the provider with Thunder Compute's current mode-less API while preserving v0.1.0 state and avoiding unexpected instance replacement.

## Highlights

- The obsolete `mode` and `allow_snapshot_modify` arguments, plus synthetic data-source `mode` fields, have been removed. Existing v0.1 state upgrades automatically after both arguments are removed from configuration.
- GPU specs, availability, and pricing now use canonical v2 keys such as `h100_x1`. GPU specs also expose `ram_cap_gib`.
- HTTP ports use their dedicated PATCH endpoint. Omitted ports adopt template/server defaults, `http_ports = []` means no HTTP ports, and an explicit set is reconciled exactly.
- Instance creation now retains the returned UUID through list-consistency lag, generated private keys survive refresh/update, and unsafe automatic snapshot/recreate fallback has been removed.
- Snapshot creation is recoverable after ambiguous responses, and imports require `snapshot_id,instance_uuid` so `instance_id` is preserved.
- Retry behavior is method-aware so non-idempotent creates are not silently repeated.

## Before upgrading

Read the [v0.1.0 to v0.2.0 migration guide](https://github.com/Thunder-Compute/terraform-provider-thundercompute/blob/v0.2.0/docs/guides/version-0.2-migration.md), back up state, upgrade the provider constraint, and review `terraform plan` before applying.

Sensitive values such as `generated_key` remain present in Terraform state even though CLI output is redacted. Use encrypted remote state with restricted access, and prefer a user-supplied public key.

Legacy instances that return `unsupported_instance_version` are not replaced automatically. Snapshot and recreate them manually when you are ready to accept new instance identity, networking, and key material.

## Release integrity

All platform binaries are built and packaged by the pinned GoReleaser workflow using the Go toolchain declared by `go.mod`. The release contains the same 13-platform matrix as v0.1.0, a protocol 6.0 Registry manifest, SHA-256 checksums, and a detached GPG signature over the checksum file.
