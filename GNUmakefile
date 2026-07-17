BINARY_NAME  = terraform-provider-thundercompute
HOSTNAME     = registry.terraform.io
NAMESPACE    = Thunder-Compute
NAME         = thundercompute
VERSION      := $(shell tr -d '[:space:]' < VERSION)
TFPLUGINDOCS_VERSION := v0.24.0
OS_ARCH      = $(shell go env GOOS)_$(shell go env GOARCH)
INSTALL_DIR  = ~/.terraform.d/plugins/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$(OS_ARCH)

default: build

build:
	go build -o $(BINARY_NAME)

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY_NAME) $(INSTALL_DIR)/

# Tests must run through Bazel (repository rule; never invoke go test directly).
test:
	bazel test //terraform/... --test_output=errors --nocache_test_results

testacc:
	TF_ACC=1 bazel test //terraform/... --test_output=streamed --nocache_test_results --test_timeout=1800

vet:
	go vet ./...

generate-docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@$(TFPLUGINDOCS_VERSION) generate --provider-name=terraform-provider-thundercompute --rendered-provider-name="Thunder Compute"

clean:
	rm -f $(BINARY_NAME)

.PHONY: default build install test testacc vet generate-docs clean
