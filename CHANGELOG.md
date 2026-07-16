# Changelog

## 0.2.0 (Unreleased)

### Added

- Added `thundercompute_gpu_availability`, backed by the canonical v2 status contract.
- Exposed `ram_cap_gib` in `thundercompute_gpu_specs`.
- Added Bazel build, unit-test, and framework/state-test paths, a pinned documentation tool, and a local GoReleaser snapshot.
- Added a v0.1.0 state upgrader that preserves supported resource state while removing obsolete fields, plus a focused v0.2.0 migration guide.
- Added recoverable snapshot creation and compound snapshot imports in the form `snapshot_id,instance_uuid`.

### Changed

- Removed the obsolete `mode` and `allow_snapshot_modify` instance arguments. Existing v0.1.0 state is upgraded automatically, but configuration must remove both arguments when upgrading the provider constraint.
- Removed synthetic `mode` fields from instance and GPU-spec data sources and moved discovery to v2 canonical keys such as `h100_x1`.
- Normalized `api_url` to an API root while continuing to accept existing values ending in `/v1` or `/v2`.
- HTTP-port changes use the current dedicated endpoint independently from compute changes; omitted ports adopt server defaults, an empty set removes all ports, and a non-empty set is reconciled exactly.
- Restricted `num_gpus` to 1, 2, 4, or 8 and added planning validation for GPU/count combinations, CPU options, storage bounds, disk decreases, port 22, and invalid ports.
- Removed the destructive snapshot/recreate fallback. Legacy instances now fail closed with manual snapshot/recreate guidance.
- Switched provider tests to Bazel, pinned `tfplugindocs` as a Go tool dependency, and retained GoReleaser for the established 13-platform release lifecycle.

### Fixed

- Preserved a newly created instance UUID through API list-consistency lag and distinguished temporary create-time absence from confirmed external deletion.
- Preserved generated private keys across refresh and update without exposing them in diagnostics.
- Made retries method-aware: safe GETs and idempotent port PATCH requests may retry transient failures; non-idempotent create, snapshot, and key POST requests are not automatically retried.
- Preserved structured API error types such as `unsupported_instance_version` without triggering destructive fallback.
- Added snapshot disappearance/failed-state coverage and regression coverage for organization and instance SSH-key behavior.
- Accepted newline-terminated OpenSSH public key file content while rejecting embedded multiline data.

### Release process

- Made the standalone tag workflow the sole GitHub/Registry publisher and converted the monorepo workflow to validation-only.
- Added clean-main, semantic-version, existing-tag, Bazel, generated-doc, exact subtree-tree, and explicit confirmation gates.
- Replaced unguarded force pushes with normal fast-forwards or an exact `--force-with-lease`, and made standalone main plus tag updates atomic.
- Added a non-publishing dry run that builds the configured release matrix and verifies the native packaged provider's embedded version.
- Pinned release workflow actions to immutable commits and retained GPG checksum signing.

## 0.1.0 (2026-04-10)

### Added

- Added `thundercompute_instance`, `thundercompute_instance_key`, `thundercompute_ssh_key`, and `thundercompute_snapshot` resources.
- Added instances, GPU specs, pricing, and templates data sources.

### Notes

- The v0.1.0 provider exposed the legacy `mode` and `allow_snapshot_modify` contract. Read the v0.2.0 migration guide before upgrading existing state.
- The Thunder Compute API cannot remove SSH keys from instances. Destroying `thundercompute_instance_key` removes only its Terraform state record.
