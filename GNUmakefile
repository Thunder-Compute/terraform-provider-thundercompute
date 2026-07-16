HOSTNAME    := registry.terraform.io
NAMESPACE   := Thunder-Compute
NAME        := thundercompute
VERSION     := $(shell tr -d '[:space:]' < VERSION)
BINARY_NAME := terraform-provider-thundercompute_v$(VERSION)
DOCS_TOOL   := go -C tools tool tfplugindocs
GORELEASER  ?= goreleaser
GORELEASER_VERSION ?= 2.17.0

default: build

build:
	go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o "$(BINARY_NAME)" .

install: build
	@platform=$$(terraform version -json | jq -r '.platform'); \
	install_dir="$$HOME/.terraform.d/plugins/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$$platform"; \
	mkdir -p "$$install_dir"; \
	cp $(BINARY_NAME) "$$install_dir/"; \
	echo "Installed $(BINARY_NAME) in $$install_dir"

# Tests must run through Bazel (repository rule; never invoke go test directly).
test:
	./scripts/bazelw test //terraform/internal/... //terraform:version_test --stamp

testacc:
	@test "$${THUNDER_ALLOW_BILLABLE_TESTS:-}" = 1 || { echo "Set THUNDER_ALLOW_BILLABLE_TESTS=1 to acknowledge billable acceptance tests." >&2; exit 1; }
	@test -n "$${TNR_API_TOKEN:-}" || { echo "TNR_API_TOKEN is required for acceptance tests." >&2; exit 1; }
	./scripts/bazelw test //terraform/internal/... \
		--test_env=TF_ACC=1 \
		--test_env=THUNDER_ALLOW_BILLABLE_TESTS=1 \
		--test_env=TNR_API_TOKEN \
		--test_timeout=1800

vet:
	./scripts/bazelw build //terraform/...

generate-docs:
	terraform fmt -recursive examples
	$(DOCS_TOOL) generate --provider-dir=.. --provider-name=thundercompute

check-docs:
	terraform fmt -check -recursive examples
	$(DOCS_TOOL) generate --provider-dir=.. --provider-name=thundercompute
	git diff --exit-code HEAD -- docs examples templates go.mod go.sum tools/go.mod tools/go.sum
	@untracked=$$(git ls-files --others --exclude-standard -- docs examples templates go.mod go.sum tools/go.mod tools/go.sum); \
	if [ -n "$$untracked" ]; then \
		echo "Generated Terraform documentation contains untracked files:" >&2; \
		printf '%s\n' "$$untracked" >&2; \
		exit 1; \
	fi

check-goreleaser:
	@actual=$$($(GORELEASER) --version 2>&1 | awk '$$1 == "GitVersion:" { print $$2; exit }'); \
	actual=$${actual#v}; \
	if [ "$$actual" != "$(GORELEASER_VERSION)" ]; then \
		echo "GoReleaser $(GORELEASER_VERSION) is required, got '$${actual:-unknown}'" >&2; \
		exit 1; \
	fi

package-dry-run: check-goreleaser
	$(GORELEASER) check
	GORELEASER_CURRENT_TAG=v$(VERSION) $(GORELEASER) release --snapshot --clean --skip=sign
	@platform=$$(terraform version -json | jq -r '.platform'); \
	archive="dist/terraform-provider-thundercompute_$(VERSION)_$${platform}.zip"; \
	binary="$(BINARY_NAME)"; \
	case "$$platform" in windows_*) binary="$$binary.exe" ;; esac; \
	tmp=$$(mktemp -d); \
	trap 'rm -rf "$$tmp"' 0; \
	unzip -q "$$archive" "$$binary" -d "$$tmp"; \
	got=$$("$$tmp/$$binary" -version); \
	if [ "$$got" != "$(VERSION)" ]; then \
		echo "Packaged provider version is '$$got', want '$(VERSION)'" >&2; \
		exit 1; \
	fi

update-bazel-lock:
	TERRAFORM_FORCE_STANDALONE_BAZEL=1 \
	TERRAFORM_BAZEL_LOCK_OUTPUT=$(CURDIR)/release/bazel/MODULE.bazel.lock \
	./scripts/bazelw mod deps

clean:
	rm -f terraform-provider-thundercompute_v*
	rm -rf dist

.PHONY: default build install test testacc vet generate-docs check-docs check-goreleaser package-dry-run update-bazel-lock clean
