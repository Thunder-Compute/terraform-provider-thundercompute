---
page_title: "Migrating Thunder Compute Provider v0.1.0 to v0.2.0"
description: |-
  State-safe migration guidance for removed fields, canonical discovery keys, HTTP ports, private keys, snapshots, and legacy instances.
---

# Migrating from v0.1.0 to v0.2.0

Provider v0.2 removes the obsolete instance `mode` and `allow_snapshot_modify` arguments and the synthetic `mode` fields from data sources. Back up your Terraform state, update the provider constraint and HCL together, run `terraform init -upgrade`, and inspect the complete plan before applying.

## Remove obsolete fields from configuration

Provider v0.1 requires `mode`, while provider v0.2 rejects it as unsupported. Remove both legacy instance arguments in the same change that updates the provider constraint:

```terraform
mode                  = "prototyping" # remove
allow_snapshot_modify = true           # remove
```

The v0.1 state upgrader retains supported resource data, including the instance UUID and generated private key, while discarding only `mode` and `allow_snapshot_modify`. This state conversion alone should not destroy or replace a normal existing instance.

## Use canonical v2 discovery keys

`thundercompute_gpu_specs`, `thundercompute_gpu_availability`, and `thundercompute_pricing` use the same canonical GPU/count keys, for example:

```hcl
data "thundercompute_gpu_specs" "current" {}
data "thundercompute_gpu_availability" "current" {}
data "thundercompute_pricing" "current" {}

locals {
  configuration = "h100_x1"
  spec          = data.thundercompute_gpu_specs.current.specs[local.configuration]
  availability  = data.thundercompute_gpu_availability.current.specs[local.configuration]
  hourly_price  = data.thundercompute_pricing.current.pricing[local.configuration]
  ram_cap_gib   = local.spec.ram_cap_gib
}
```

Remove expressions that read `.mode` from `thundercompute_instances` or `thundercompute_gpu_specs` results, and strip old mode suffixes from discovery-key references (for example, replace `h100_x1_prototyping` with `h100_x1`).

## Choose HTTP-port intent explicitly

The `http_ports` value has three distinct meanings:

- Omit it (or leave it null) to adopt template, snapshot, or server defaults.
- Set `http_ports = []` to reconcile the instance to no public HTTP ports.
- Set a non-empty collection to reconcile exactly to that set.

Port 22 and values outside 1 through 65535 are rejected during planning. Compute changes and port changes are separate API operations, even when they occur in one apply.

## Protect generated private keys and state

When no `public_key` is supplied, the API may return an SSH private key in `generated_key`. v0.2.0 preserves it across refresh and update and marks it Sensitive. Sensitive values are still stored in Terraform state.

Use encrypted remote state with narrowly scoped access. Prefer supplying a public key so Terraform does not need to retain generated private key material:

```hcl
public_key = file(pathexpand("~/.ssh/id_ed25519.pub"))
```

The API cannot revoke a key added by `thundercompute_instance_key`. Destroying that resource removes only the Terraform state record; the key remains authorized on the instance.

## Handle legacy instances manually

`allow_snapshot_modify` no longer exists. The provider never snapshots, deletes, and recreates an instance automatically.

If an update reports `unsupported_instance_version`, Terraform fails closed and leaves the instance intact. At a maintenance window:

1. Create and wait for a snapshot.
2. Record networking, ports, and authorized keys.
3. Declare a replacement instance from the snapshot with the required disk size and current GPU/count configuration.
4. Review the plan, then deliberately replace or move consumers to the new instance.
5. Remove the legacy instance only after validating the replacement.

This manual boundary makes changes to UUID, IP address, SSH port, and key material explicit.

## Import snapshots with their instance UUID

Snapshot imports require the compound form:

```shell
terraform import thundercompute_snapshot.example <snapshot-id>,<instance-uuid>
```

If existing snapshot state came from an ID-only import and has no `instance_id`, back up state, remove only the Terraform state record with `terraform state rm`, and immediately re-import it with the snapshot ID and source instance UUID. Do not apply between removal and re-import; `terraform state rm` does not delete the remote snapshot.
