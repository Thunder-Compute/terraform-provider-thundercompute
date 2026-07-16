# Thunder Compute Terraform Provider

The official Terraform provider for managing [Thunder Compute](https://www.thundercompute.com) GPU instances, snapshots, organization SSH keys, and instance SSH-key authorization.

## Using the provider

Declare the Registry source as `Thunder-Compute/thundercompute`:

```hcl
terraform {
  required_providers {
    thundercompute = {
      source  = "Thunder-Compute/thundercompute"
      version = "~> 0.2.0"
    }
  }
}
```

Set `TNR_API_TOKEN` or configure the sensitive `api_token` provider attribute:

```shell
export TNR_API_TOKEN="your-api-token"
```

The default API root is `https://api.thundercompute.com:8443`. Existing `api_url` values ending in `/v1` or `/v2` remain valid and are normalized to the same root. Instance, snapshot, SSH-key, and template operations use v1 endpoints; specs, status, and pricing use v2 endpoints.

See the [v0.1.0 to v0.2.0 migration guide](docs/guides/version-0.2-migration.md) before upgrading existing state.

## Development

The authoritative source is the `terraform/` directory in the Thundernetes monorepo. It is exported to [Thunder-Compute/terraform-provider-thundercompute](https://github.com/Thunder-Compute/terraform-provider-thundercompute) with `git subtree split`.

Provider tests use Bazel 8.5.1 with its pinned Go 1.25.3 toolchain. Direct Go compilation is limited to the Terraform-specific local build, upstream `tfplugindocs`, and GoReleaser workflows; do not use direct `go test`. The documentation tool is pinned in `tools/go.mod`, and GoReleaser v2.17.0 is required locally and pinned in CI.

From the monorepo root:

```shell
bazel build //terraform:terraform --stamp
bazel test //terraform/internal/... //terraform:version_test --stamp
make terraform-check-docs
make terraform-package-dry-run
```

`make terraform-release-validate` also runs the shared Thunder type, API handler, and focused provider API contract targets. `make terraform-release-dry-run` adds the remote tag and exact subtree checks used immediately before a release.

From the standalone repository:

```shell
make build
make test
make check-docs
make package-dry-run
```

Documentation generation uses the standard, module-pinned `tfplugindocs` v0.24.0 flow and requires Go and Terraform CLI. `make check-docs` regenerates documentation in place and fails when the resulting documentation, examples, templates, or tool metadata changes are not committed.

Package dry runs require GoReleaser v2.17.0, Terraform CLI, `jq`, and `unzip`. They build the complete configured snapshot and execute the native packaged provider to verify its embedded version.

When the standalone Bazel dependency graph changes, refresh its checked-in lock file with `make update-bazel-lock`.

Acceptance tests can create billable resources. They require both a token and an explicit acknowledgement:

```shell
TNR_API_TOKEN=... THUNDER_ALLOW_BILLABLE_TESTS=1 make testacc
```

Do not run acceptance tests against a real account without authorization.

## State and secret handling

The `generated_key` attribute is marked Sensitive, but Terraform still stores its value in state. Use encrypted remote state with tightly scoped access. Prefer supplying your own public key with `public_key = file(pathexpand("~/.ssh/id_ed25519.pub"))` so Terraform does not need to retain an auto-generated private key.

The Thunder Compute API cannot revoke a key added directly to an instance. Destroying `thundercompute_instance_key` removes only the Terraform state record; the key remains authorized on the instance.

## Release process

`VERSION` is the only human-edited provider version. The tag must be exactly `v${VERSION}`, and GoReleaser injects that value into every published platform binary.

The standalone repository's tag-triggered `.github/workflows/release.yml` is the sole publisher. GoReleaser builds the existing 13-platform Registry matrix, creates deterministic ZIP archives, includes the protocol 6.0 manifest, signs the checksum file with GPG, and creates the GitHub release with substantive release notes.

The monorepo pull-request workflow is local validation only and never queries or updates the standalone repository. Run the same local gates with:

```shell
make terraform-release-validate
```

A complete release preflight, including read-only remote and subtree checks, is:

```shell
make terraform-release-dry-run
```

The real `make deploy-terraform` path requires `main`, a completely clean tree, a version and tag that do not already exist, passing local release validation, an exact subtree-tree comparison, and an explicit confirmation. It atomically pushes standalone `main` and the tag using either a normal fast-forward or an exact `--force-with-lease`. It never dispatches a second publisher.
