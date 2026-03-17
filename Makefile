GOPATH          := $(shell go env GOPATH)
BIN_DIR         := $(GOPATH)/bin

GOFUMPT         := $(BIN_DIR)/gofumpt
GOLINT          := $(BIN_DIR)/golangci-lint

FFIGEN_BIN      := ./bin/ffigen

.PHONY: fmt lint lint-fix test gen clean

$(FFIGEN_BIN): cmd/ffigen/main.go
	@echo "Building ffigen tool..."
	@mkdir -p bin
	@go build -o $(FFIGEN_BIN) cmd/ffigen/main.go

gen:
	@echo "Generating FFI code with go generate..."
	@go generate ./...

clean:
	rm -rf bin
	find . -name "*_ffigen.go" -delete

test: gen
	@go test -v ./...