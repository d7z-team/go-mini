GOPATH          := $(shell go env GOPATH)
BIN_DIR         := $(GOPATH)/bin

GOFUMPT         := $(BIN_DIR)/gofumpt
GOLINT          := $(BIN_DIR)/golangci-lint

FFIGEN_BIN      := ./bin/ffigen
LSP_SERVER_BIN  := ./bin/lsp-server
EXEC_BIN        := ./bin/mini-exec
GO_TEST         := go test
GO_TEST_TIMEOUT ?= 180s
EXAMPLE_TIMEOUT ?= 30s

# 获取所有 Go 源码文件作为依赖
GO_SOURCES := $(shell find . -name "*.go" -not -path "./vendor/*" -not -path "./bin/*")

.PHONY: build build-ffigen build-lsp build-exec build-all fmt lint lint-fix test coverage gen tidy clean package-vsix examples

build: build-all

build-ffigen: $(FFIGEN_BIN)

build-lsp: $(LSP_SERVER_BIN)

build-exec: $(EXEC_BIN)

build-all: $(FFIGEN_BIN) $(LSP_SERVER_BIN) $(EXEC_BIN)

$(FFIGEN_BIN): $(GO_SOURCES)
	@echo "Building ffigen tool..."
	@mkdir -p bin
	@go build -o $(FFIGEN_BIN) ./core/cmd/ffigen

$(LSP_SERVER_BIN): $(GO_SOURCES)
	@echo "Building lsp-server..."
	@mkdir -p bin
	@go build -o $(LSP_SERVER_BIN) ./examples/cmd/lsp-server

$(EXEC_BIN): $(GO_SOURCES)
	@echo "Building mini-exec..."
	@mkdir -p bin
	@go build -o $(EXEC_BIN) ./examples/cmd/exec

package-vsix: $(LSP_SERVER_BIN) $(EXEC_BIN)
	@echo "Packaging VSCode extension..."
	@mkdir -p vscode-ext/bin
	@cp $(LSP_SERVER_BIN) vscode-ext/bin/lsp-server
	@cp $(EXEC_BIN) vscode-ext/bin/mini-exec
	@cd vscode-ext && npm install && NODE_NO_WARNINGS=1 ./node_modules/.bin/vsce package -o ../go-mini.vsix
	@echo "Successfully packaged to go-mini.vsix"

examples: $(EXEC_BIN)
	@echo "Running example scripts..."
	@find examples -type f -name "*.mgo" | sort | while read -r file; do \
		echo "==> $$file"; \
		timeout $(EXAMPLE_TIMEOUT) $(EXEC_BIN) -run "$$file" || exit 1; \
	done

gen:
	@echo "Generating FFI code with go generate..."
	@cd core && go generate ./...
	@cd ffilib && go generate ./...
	@cd examples && go generate ./...

tidy:
	@cd core && go mod tidy
	@cd ffilib && go mod tidy
	@cd examples && go mod tidy

clean:
	rm -rf bin

fmt: gen
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	"$(GOPATH)/bin/golangci-lint" fmt -c .golangci.yml
	@find . -name "*.mgo" -not -path "./vendor/*" -not -path "./bin/*" -exec gofmt -w {} +
	@$(MAKE) tidy

lint: gen
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	cd core && "$(GOPATH)/bin/golangci-lint" run -c ../.golangci.yml ./... && \
	cd ../ffilib && "$(GOPATH)/bin/golangci-lint" run -c ../.golangci.yml ./... && \
	cd ../examples && "$(GOPATH)/bin/golangci-lint" run -c ../.golangci.yml ./...

lint-fix: gen
	@(test -f "$(GOPATH)/bin/golangci-lint" || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.6.0) && \
	cd core && "$(GOPATH)/bin/golangci-lint" run -c ../.golangci.yml ./... --fix && \
	cd ../ffilib && "$(GOPATH)/bin/golangci-lint" run -c ../.golangci.yml ./... --fix && \
	cd ../examples && "$(GOPATH)/bin/golangci-lint" run -c ../.golangci.yml ./... --fix

test: gen
	@cd core && $(GO_TEST) -timeout $(GO_TEST_TIMEOUT) ./...
	@cd ffilib && $(GO_TEST) -timeout $(GO_TEST_TIMEOUT) ./...
	@cd examples && $(GO_TEST) -timeout $(GO_TEST_TIMEOUT) ./...

coverage: gen
	@rm -f coverage.txt coverage-core.out coverage-ffilib.out coverage-examples.out
	@cd core && packages="$$(go list ./... | grep -v '/cmd/ffigen/tests$$')" && coverpkg="$$(printf '%s\n' "$$packages" | paste -sd, -)" && $(GO_TEST) -timeout $(GO_TEST_TIMEOUT) -coverpkg="$$coverpkg" -coverprofile=../coverage-core.out $$packages
	@cd ffilib && coverpkg="$$(go list ./... | paste -sd, -)" && $(GO_TEST) -timeout $(GO_TEST_TIMEOUT) -coverpkg="$$coverpkg" -coverprofile=../coverage-ffilib.out ./...
	@cd examples && coverpkg="$$(go list ./... | paste -sd, -)" && $(GO_TEST) -timeout $(GO_TEST_TIMEOUT) -coverpkg="$$coverpkg" -coverprofile=../coverage-examples.out ./...
	@echo "mode: set" > coverage.txt
	@for file in coverage-core.out coverage-ffilib.out coverage-examples.out; do tail -n +2 "$$file" >> coverage.txt; done
	@rm -f coverage-core.out coverage-ffilib.out coverage-examples.out
