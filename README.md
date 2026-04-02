# Go-Mini

Go-Mini is a Go-like scripting engine with a bytecode-first runtime.

- Compile source code to `go-mini-bytecode`
- Run prepared programs without AST on the main runtime path
- Generate schema-only FFI bindings with `cmd/ffigen`

## Install

```bash
go install gopkg.d7z.net/go-mini/cmd/exec@latest
go install gopkg.d7z.net/go-mini/cmd/ffigen@latest
```

## Quick Start

Run a script:

```bash
go run ./cmd/exec -run script.go
```

Compile to bytecode:

```bash
go run ./cmd/exec -o script.json script.go
```

Run bytecode:

```bash
go run ./cmd/exec -bytecode script.json
```

Generate FFI bindings:

```bash
go run ./cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
```

## Development

```bash
GOCACHE=/tmp/go-build-cache go test ./core/runtime
GOCACHE=/tmp/go-build-cache go test ./core/e2e/...
GOCACHE=/tmp/go-build-cache go test ./...
```

## Docs

- [DOCS.md](./DOCS.md)
- [LSP.md](./LSP.md)
- [AGENTS.md](./AGENTS.md)
- [TODO.md](./TODO.md)

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
