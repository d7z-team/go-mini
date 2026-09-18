golangci_lint := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
minigo := go run ./cmd/mini-go
minigo_sources := $(shell find stdlib/src stdlib/host -type f \( -name '*.mgo' -o -name '*.mrpc' \) -print | sort) rpc/gateway/control.mrpc
syntax_fuzz_packages := ./compiler/scanner ./compiler/parser ./compiler/ast ./compiler/optimize ./compiler/workspace ./compiler ./compiler/bootstrap ./compiler/format ./compiler/analysis ./compiler/language ./tooling/mrpc
runtime_fuzz_packages := ./runtime/bytecode ./runtime ./rpc ./rpc/router ./rpc/gateway ./stdlib/... ./tooling/dap ./integrations
TEST_PACKAGES ?= ./...
TEST_FLAGS ?= -timeout=20m
RACE_PACKAGES ?= ./ffi ./runtime ./rpc/... ./compiler/cache ./compiler/workspace ./compiler/service ./compiler/language ./tooling/lsp ./tooling/dap
RACE_FLAGS ?= -timeout=10m -p=1
FUZZTIME ?= 10s
runtime_images := testdata/runtime/execution.json.gz testdata/runtime/stdlib.json.gz
compiler_image := playground/runtime-rust/tooling/assets/compiler.json.gz

.PHONY: build generate doc test race bootstrap-test chaos-syntax fuzz fuzz-syntax fuzz-runtime fmt lint cache-clean clean _compiler-identity

.PHONY: runtime-rust-test runtime-rust-lint runtime-rust-conformance runtime-rust-bench
.PHONY: runtime-wasm-build runtime-wasm-test runtime-wasm-pack
.PHONY: runtime-compiler-test
.PHONY: runtime-artifacts runtime-compiler-image

runtime-compiler-test: runtime-wasm-build
	@cargo test --locked --release --manifest-path playground/runtime-rust/Cargo.toml -p mini-go-tooling --test compiler_entry --test compiler
	@cd playground/runtime-rust/runtime-wasm && node --test tests/compiler.test.js tests/tools.test.js tests/tools_fault.test.js

.PHONY: test-rpc-conformance runtime-rust-rpc-test
.PHONY: runtime-rust-host-test runtime-rust-host-conformance

runtime-rust-host-test:
	@cargo test --locked --manifest-path playground/runtime-rust/Cargo.toml --features stdlib-host --lib --test stdlib_host

runtime-rust-host-conformance: testdata/runtime/stdlib.json.gz
	@cargo test --locked --release --manifest-path playground/runtime-rust/Cargo.toml --features stdlib-host --test stdlib -- --nocapture

runtime-rust-rpc-test:
	@cargo test --locked --manifest-path playground/runtime-rust/Cargo.toml --features rpc --test 'rpc_*' --test cancellation
	@cargo test --locked --manifest-path playground/runtime-rust/Cargo.toml --features rpc-gateway --test rpc_gateway

test-rpc-conformance:
	@GOTOOLCHAIN=go1.26.6 go build -o bin/mini-go-rpc-peer-go ./cmd/mini-go-rpc-peer-go
	@cargo build --locked --manifest-path playground/runtime-rust/Cargo.toml --features rpc-gateway --bin mini-go-rpc-peer-rust
	@MINIGO_RPC_GO_PEER="$(CURDIR)/bin/mini-go-rpc-peer-go" MINIGO_RPC_RUST_PEER="$(CURDIR)/playground/runtime-rust/target/debug/mini-go-rpc-peer-rust" GOTOOLCHAIN=go1.26.6 go test ./integrations -run '^TestRPC(Peer|Gateway)Conformance$$' -count=1 -timeout=3m

runtime-rust-bench:
	@cargo bench --locked --manifest-path playground/runtime-rust/Cargo.toml --bench runtime

runtime-rust-test: runtime-artifacts
	@cargo test --locked --manifest-path playground/runtime-rust/Cargo.toml
	@cargo test --locked --release --manifest-path playground/runtime-rust/Cargo.toml -p mini-go-tooling

runtime-rust-lint: runtime-artifacts
	@cargo fmt --all --manifest-path playground/runtime-rust/Cargo.toml --check
	@cargo clippy --locked --manifest-path playground/runtime-rust/Cargo.toml --workspace --all-features --all-targets -- -D warnings

runtime-rust-conformance: testdata/runtime/stdlib.json.gz
	@GOTOOLCHAIN=go1.26.6 go build -o bin/mini-go-dev ./cmd/mini-go-dev
	@MINIGO_HOST_BROKER="$(CURDIR)/bin/mini-go-dev" cargo test --locked --release --manifest-path playground/runtime-rust/Cargo.toml --features host-conformance --lib --test stdlib -- --nocapture

runtime-wasm-build:
	npm ci --prefix playground/runtime-rust/runtime-wasm
	npm run build --prefix playground/runtime-rust/runtime-wasm

runtime-wasm-test: runtime-wasm-build testdata/runtime/execution.json.gz
	npm run lint --prefix playground/runtime-rust/runtime-wasm
	GOTOOLCHAIN=go1.26.6 go build -o bin/mini-go-rpc-peer-go ./cmd/mini-go-rpc-peer-go
	MINIGO_WASM_FIXTURES="$(CURDIR)/playground/runtime-rust/target/wasm-fixtures" cargo test --locked --manifest-path playground/runtime-rust/Cargo.toml --test wasm_driver
	MINIGO_WASM_FIXTURES="$(CURDIR)/playground/runtime-rust/target/wasm-fixtures" MINIGO_RPC_GO_PEER="$(CURDIR)/bin/mini-go-rpc-peer-go" npm test --prefix playground/runtime-rust/runtime-wasm

runtime-wasm-pack:
	npm ci --prefix playground/runtime-rust/runtime-wasm
	cd playground/runtime-rust/runtime-wasm && npm pack

runtime-artifacts: $(runtime_images) $(compiler_image)

runtime-compiler-image: $(compiler_image)

$(runtime_images) $(compiler_image): | _compiler-identity

testdata/runtime/execution.json.gz:
	@GOTOOLCHAIN=go1.26.6 go generate -run 'runtime-vectors ' .

testdata/runtime/stdlib.json.gz:
	@GOTOOLCHAIN=go1.26.6 go generate -run 'runtime-stdlib-vectors ' .

$(compiler_image):
	@GOTOOLCHAIN=go1.26.6 go generate -run 'mini-go-dev bootstrap ' .

_compiler-identity:
	@go generate -run compiler-identity .

build: _compiler-identity
	@go build ./...
	@go build -o bin/ ./cmd/...

generate:
	@GOTOOLCHAIN=go1.26.6 go generate .

doc:
	@$(minigo) doc -out docs/reference std

test: _compiler-identity $(runtime_images)
	@go test $(TEST_FLAGS) $(TEST_PACKAGES)

race: _compiler-identity $(runtime_images)
	@go test -race $(RACE_FLAGS) $(RACE_PACKAGES)

bootstrap-test: _compiler-identity
	@go test -timeout=5m -count=1 ./compiler/bootstrap -run '^TestCompilerImage(CompilesAndRunsSource|MatchesNativeCorpus)$$'

chaos-syntax: _compiler-identity
	@go test ./compiler -run '^TestSyntax' -count=1

define run_fuzz_packages
	@set -eu; \
	for package in $$(go list $(1)); do \
		targets="$$(go test "$$package" -run '^$$' -list '^Fuzz')"; \
		for target in $$targets; do \
			case "$$target" in \
				Fuzz*) \
					printf 'fuzz %s %s\n' "$$package" "$$target"; \
					go test "$$package" -run '^$$' -fuzz "^$${target}$$" -fuzztime=$(FUZZTIME);; \
			esac; \
		done; \
	done
endef

fuzz: fuzz-syntax fuzz-runtime

fuzz-syntax:
	$(call run_fuzz_packages,$(syntax_fuzz_packages))

fuzz-runtime: $(runtime_images)
	$(call run_fuzz_packages,$(runtime_fuzz_packages))

fmt:
	@$(golangci_lint) fmt --config .golangci.yml
	@$(minigo) fmt -w $(minigo_sources)

lint: _compiler-identity
	@bash scripts/check-boundaries.sh
	@$(minigo) fmt -check $(minigo_sources)
	@$(minigo) doc -out docs/reference -check std
	@$(golangci_lint) run --config .golangci.yml ./...

cache-clean:
	@$(minigo) cache clean

clean: cache-clean
	@$(RM) -r bin .cache build
	@go clean -testcache -fuzzcache
