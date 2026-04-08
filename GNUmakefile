BINARY_NAME  = terraform-provider-thundercompute
HOSTNAME     = registry.terraform.io
NAMESPACE    = Thunder-Compute
NAME         = thundercompute
VERSION      = 0.1.0
OS_ARCH      = $(shell go env GOOS)_$(shell go env GOARCH)
INSTALL_DIR  = ~/.terraform.d/plugins/$(HOSTNAME)/$(NAMESPACE)/$(NAME)/$(VERSION)/$(OS_ARCH)

default: build

build:
	go build -o $(BINARY_NAME)

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY_NAME) $(INSTALL_DIR)/

test:
	go test ./internal/... -v -count=1

testacc:
	TF_ACC=1 go test ./internal/... -v -count=1 -timeout 30m

vet:
	go vet ./...

generate-docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate

clean:
	rm -f $(BINARY_NAME)

.PHONY: default build install test testacc vet generate-docs clean
