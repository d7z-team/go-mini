GOPATH          := $(shell go env GOPATH)
BIN_DIR         := $(GOPATH)/bin

GOFUMPT         := $(BIN_DIR)/gofumpt
GOLINT          := $(BIN_DIR)/golangci-lint

.PHONY: fmt lint lint-fix test gen

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

gen:
	@echo "Generating FFI code for E2E tests..."
	@go run cmd/ffigen/main.go -pkg e2e -out core/e2e/dummy_ffigen.go core/e2e/dummy.go
	@go run cmd/ffigen/main.go -pkg e2e -out core/e2e/ffi_struct_ffigen.go core/e2e/ffi_struct_test.go
	@go run cmd/ffigen/main.go -pkg e2e -out core/e2e/ffi_variadic_ffigen.go core/e2e/ffi_variadic_test.go

test: gen
	@go test -v ./...