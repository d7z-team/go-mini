# Go-Mini

Go-Mini is a Go-like scripting engine for embedding, bytecode execution, and schema-based FFI.

## Features

- Go-like syntax for embedded scripts
- Bytecode-first compile and execution flow
- Embeddable Go API
- Schema-based FFI with generated bindings
- Language support for functions, structs, interfaces, channels, select, errors, schema-backed reflect, and modules
- Optional standard-library FFI surface for packages such as `fmt`, `time`, `context`, `io`, and `os`
- CLI examples and LSP helper APIs

## Install

```bash
go install gopkg.d7z.net/go-mini/core/cmd/ffigen@latest
```

## CLI

Run a script:

```bash
go run ./examples/cmd/exec -run script.mgo
```

Compile and run bytecode:

```bash
go run ./examples/cmd/exec -o script.json script.mgo
go run ./examples/cmd/exec -bytecode script.json
```

## Embedding

```go
package main

import (
	"context"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/ffilib"
)

func main() {
	exec, err := engine.NewMiniExecutor()
	if err != nil {
		panic(err)
	}
	if err := exec.UseSurface(ffilib.Surface()); err != nil {
		panic(err)
	}

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
go run gopkg.d7z.net/go-mini/core/cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
```

Generated bindings expose `SurfaceXxx(...) *surface.Bundle` for `executor.UseSurface(...)`. See [DOCS.md](./DOCS.md) for runtime integration details.

## Development

```bash
make lint test examples
```

The repository uses a multi-module layout with `core`, `ffilib`, and `examples`, coordinated by the root `go.work`.

## Documentation

- [DOCS.md](./DOCS.md): usage and architecture details
- [LSP.md](./LSP.md): editor and LSP integration
- [TODO.md](./TODO.md): current architecture state and remaining work

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
