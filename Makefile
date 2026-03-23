GOPATH          := $(shell go env GOPATH)
BIN_DIR         := $(GOPATH)/bin

GOFUMPT         := $(BIN_DIR)/gofumpt
GOLINT          := $(BIN_DIR)/golangci-lint

FFIGEN_BIN      := ./bin/ffigen
LSP_SERVER_BIN  := ./bin/lsp-server
EXEC_BIN        := ./bin/mini-exec

# 获取所有 Go 源码文件作为依赖
GO_SOURCES := $(shell find . -name "*.go" -not -path "./vendor/*" -not -path "./bin/*")

.PHONY: build build-ffigen build-lsp build-exec build-all fmt lint lint-fix test gen clean package-vsix

build: build-all

build-ffigen: $(FFIGEN_BIN)

build-lsp: $(LSP_SERVER_BIN)

build-exec: $(EXEC_BIN)

build-all: $(FFIGEN_BIN) $(LSP_SERVER_BIN) $(EXEC_BIN)

$(FFIGEN_BIN): $(GO_SOURCES)
	@echo "Building ffigen tool..."
	@mkdir -p bin
	@go build -o $(FFIGEN_BIN) cmd/ffigen/main.go

$(LSP_SERVER_BIN): $(GO_SOURCES)
	@echo "Building lsp-server..."
	@mkdir -p bin
	@go build -o $(LSP_SERVER_BIN) cmd/lsp-server/main.go

$(EXEC_BIN): $(GO_SOURCES)
	@echo "Building mini-exec..."
	@mkdir -p bin
	@go build -o $(EXEC_BIN) cmd/exec/main.go

package-vsix: $(LSP_SERVER_BIN) $(EXEC_BIN)
	@echo "Packaging VSCode extension..."
	@mkdir -p vscode-ext/bin
	@cp $(LSP_SERVER_BIN) vscode-ext/bin/lsp-server
	@cp $(EXEC_BIN) vscode-ext/bin/mini-exec
	@cd vscode-ext && npm install && NODE_NO_WARNINGS=1 ./node_modules/.bin/vsce package -o ../go-mini.vsix
	@echo "Successfully packaged to go-mini.vsix"

gen:
	@echo "Generating FFI code with go generate..."
	@go generate ./...

clean:
	rm -rf bin
	find . -name "*_ffigen.go" -delete

fmt: gen
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	"$(GOPATH)/bin/golangci-lint" fmt -c .golangci.yml
	@go mod tidy

lint: gen
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	"$(GOPATH)/bin/golangci-lint" run -c .golangci.yml

lint-fix: gen
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	"$(GOPATH)/bin/golangci-lint" run -c .golangci.yml --fix

test: gen
	@go test -v -coverprofile=coverage.txt ./...