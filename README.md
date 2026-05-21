# Go-Mini

Go-Mini is a Go-like scripting engine for embedding, bytecode execution, and schema-based FFI.

## Features

- Go-like syntax for small scripts and embedded workflows
- Bytecode-first runtime
- Embeddable Go API
- CLI for running scripts and bytecode
- Compile-time call templates for lightweight builtins
- FFI binding generator
- LSP helpers for editor integrations

## Install

```bash
go install gopkg.d7z.net/go-mini/cmd/exec@latest
go install gopkg.d7z.net/go-mini/cmd/ffigen@latest
```

## CLI

Run a script:

```bash
go run ./cmd/exec -run script.mgo
```

Compile to bytecode:

```bash
go run ./cmd/exec -o script.json script.mgo
```

Run bytecode:

```bash
go run ./cmd/exec -bytecode script.json
```

## Embedding

```go
package main

import (
	"context"

	engine "gopkg.d7z.net/go-mini/core"
)

func main() {
	exec := engine.NewMiniExecutor()
	prog, err := exec.NewRuntimeByGoCode(`
package main

func main() {
	println("hello from go-mini")
}
`)
	if err != nil {
		panic(err)
	}
	if err := prog.Execute(context.Background()); err != nil {
		panic(err)
	}
}
```

## FFI

Generate bindings from Go interfaces:

```bash
go run ./cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
```

For examples and runtime integration details, see [DOCS.md](./DOCS.md).

## Development

```bash
make lint test
```

Useful focused checks:

```bash
GOCACHE=/tmp/go-build-cache go test -timeout 180s ./core/runtime
GOCACHE=/tmp/go-build-cache go test -timeout 180s ./...
```

## Documentation

- [DOCS.md](./DOCS.md)
- [LSP.md](./LSP.md)
- [TODO.md](./TODO.md)

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
