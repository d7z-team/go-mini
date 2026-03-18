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

fmt:
	$(call ensure_tool,$(GOFUMPT),mvdan.cc/gofumpt@latest)
	$(GOFUMPT) -l -w .
	go mod tidy

lint:
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	"$(GOPATH)/bin/golangci-lint" run -c .golangci.yml

lint-fix:
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	"$(GOPATH)/bin/golangci-lint" run -c .golangci.yml --fix