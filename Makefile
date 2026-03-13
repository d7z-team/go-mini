GOPATH          := $(shell go env GOPATH)
BIN_DIR         := $(GOPATH)/bin

GOFUMPT         := $(BIN_DIR)/gofumpt
GOLINT          := $(BIN_DIR)/golangci-lint

.PHONY: fmt lint lint-fix

fmt:
	$(call ensure_tool,$(GOFUMPT),mvdan.cc/gofumpt@latest)
	$(GOFUMPT) -l -w .
	go mod tidy

lint:
	@$(call ensure_tool,$(GOLINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest)
	$(GOLINT) run -c .golangci.yml

lint-fix:
	@$(call ensure_tool,$(GOLINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest)
	$(GOLINT) run -c .golangci.yml --fix

test:
	@go test -v ./...