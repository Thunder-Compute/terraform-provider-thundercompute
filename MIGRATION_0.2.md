# Migrating from provider v0.1 to v0.2

Provider v0.2 models the current Thunder Compute API. It removes the obsolete instance `mode` and `allow_snapshot_modify` arguments, removes synthetic `mode` values from data sources, and uses canonical discovery keys.

Upgrade the provider constraint and configuration in the same change. Provider v0.1 requires `mode`, while provider v0.2 rejects it as an unsupported argument, so configuration cannot be migrated independently while continuing to plan with v0.1.

## Before upgrading

1. Back up or version the Terraform state using the configured backend. Protect the backup as a secret: `generated_key` can contain an SSH private key in both active state and state backups.
2. Make sure no other Terraform operation is running against the same state.
3. Note the snapshot ID and source instance UUID for any snapshot that was originally imported with only a snapshot ID.

## Update provider configuration and HCL

Update the provider constraint to v0.2 and, in the same change, remove these arguments from every `thundercompute_instance` resource:

```hcl
mode                  = "prototyping" # remove
allow_snapshot_modify = true           # remove
```

Also remove expressions that read `.mode` from `thundercompute_instances` or `thundercompute_gpu_specs` data-source results.

Discovery data now uses only the canonical keys returned by the current API. Strip the old mode suffix from references, for example:

```text
h100_x1_prototyping -> h100_x1
h100_x4_production  -> h100_x4
```

`allow_snapshot_modify` no longer exists. If the API reports that an existing instance version cannot be modified in place, create a snapshot and recreate the instance explicitly; the provider will not perform a destructive snapshot-and-recreate fallback.

Existing v0.1 instance state is decoded automatically. The v0.2 state upgrade retains supported resource data, including the instance UUID and generated private key, while discarding only `mode` and `allow_snapshot_modify`. This state conversion alone should not destroy or replace a normal existing instance.

## Re-import snapshots that used an ID-only import

Provider v0.2 requires snapshot imports in the form `snapshot_id,instance_uuid`. If existing snapshot state has no `instance_id`, migrate it without applying in between:

```shell
# First create and securely store a state backup using your backend's process.
terraform state rm 'thundercompute_snapshot.example'
terraform import 'thundercompute_snapshot.example' 'snapshot_id,instance_uuid'
terraform plan
```

`terraform state rm` only makes Terraform forget the snapshot; it does not delete the remote snapshot. Do not run `terraform apply` between the state removal and the compound re-import. Ensure the resource configuration contains the same `instance_id` used in the import command.

## Complete the upgrade

After updating the provider constraint and HCL together:

```shell
terraform init -upgrade
terraform plan
```

Review the complete plan. Stop and investigate if Terraform proposes any unexpected destroy or replacement before applying.
