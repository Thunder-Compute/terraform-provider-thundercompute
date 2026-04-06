# Terraform Provider Thunder Compute

Terraform provider for managing [Thunder Compute](https://thundercompute.com) GPU cloud resources: instances, SSH keys, and snapshots.

## Requirements

- [Terraform](https://developer.hashicorp.com/terraform/downloads) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.26 (to build the provider)

## Authentication

Set the `TNR_API_TOKEN` environment variable or configure `api_token` directly:

```shell
export TNR_API_TOKEN="your-api-token"
```

Generate tokens at [console.thundercompute.com/settings/tokens](https://console.thundercompute.com/settings/tokens).

## Building & Installing Locally

```shell
make install
```

This compiles the provider and places the binary into `~/.terraform.d/plugins/` under the `local` namespace.

## Provider Address & Registry Publishing

The provider address in `main.go` is currently set to:

```
registry.terraform.io/local/thundercompute
```

The `local` namespace is for **local development only**. Before publishing to the Terraform Registry, update the address to your organization's namespace:

```
registry.terraform.io/<YOUR_GITHUB_ORG>/thundercompute
```

For example, if your GitHub organization is `thunder-compute`:

```
registry.terraform.io/thunder-compute/thundercompute
```

## Release Workflow

Releases are built with [GoReleaser](https://goreleaser.com/) and published to GitHub Releases.

### How it works

1. Tag a version: `git tag v0.1.0 && git push origin v0.1.0`
2. GoReleaser builds binaries, creates checksums, signs with GPG, and creates a **GitHub Release**.
3. The release is created as a **published** (non-draft) release.
4. If you have registered the provider with the [Terraform Registry](https://registry.terraform.io/), a webhook on the `release` event will automatically notify the registry of the new version.

### Important notes

- **No auto-publish to Terraform Registry** happens unless you have explicitly registered the provider and configured the GitHub webhook via the Terraform Registry UI.
- To register: sign in at [registry.terraform.io](https://registry.terraform.io/) with your GitHub account, then select **Publish > Provider**.

### GPG signing

Set `GPG_FINGERPRINT` environment variable to your GPG key fingerprint before running GoReleaser. The Terraform Registry requires GPG-signed checksums.

## API Usage

This provider communicates with the Thunder Compute API at `https://api.thundercompute.com:8443/v1`. The HTTP client includes:

- **Automatic retry** for transient failures (5xx errors, network issues) with exponential backoff (up to 3 retries).
- **TLS 1.2 minimum** for transport security.
- **Response size limit** of 10MB to prevent memory exhaustion.

Please use the provider responsibly. Polling operations (instance creation, snapshot waiting) use 5-second intervals. Avoid running excessively parallel applies against the same Thunder Compute account.

## Developing

```shell
# Run unit tests
make test

# Run acceptance tests (requires TNR_API_TOKEN)
make testacc

# Generate documentation
make generate-docs

# Lint
make vet
```

## Documentation

Provider documentation is auto-generated with [terraform-plugin-docs](https://github.com/hashicorp/terraform-plugin-docs) from the `templates/` and `examples/` directories. Run `make generate-docs` to regenerate the `docs/` directory.
