# Go-Mini

Go-Mini is a Go-like scripting engine for embedding, bytecode execution, and schema-based FFI.

## Features

- Go-like syntax for small scripts and embedded workflows
- Bytecode-first runtime
- Frontend boundary for Go and future source languages
- Embeddable Go API
- CLI for running scripts and bytecode
- Compile-time call templates for lightweight builtins
- Surface bundles for FFI and VM source libraries
- Surface-packaged VM source libraries with explicit exports and bytecode hash validation
- Language-level `chan` / `select` with cooperative VM scheduling
- VM-only pointer semantics with `new`, `&`, dereference, and struct literals
- FFI binding generator
- Cooperative VM scheduling with async FFI all-blocked diagnostics
- Built-in core standard-library subset for errors and pure value helpers
- LSP helpers for editor integrations

## Install

```bash
go install gopkg.d7z.net/go-mini/core/cmd/ffigen@latest
```

## CLI

Run a script:

```bash
go run ./examples/cmd/exec -run script.mgo
```

Compile to bytecode:

```bash
go run ./examples/cmd/exec -o script.json script.mgo
```

Run bytecode:

```bash
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
	exec := engine.NewMiniExecutor()
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

Generated bindings expose `SurfaceXxx(...) *surface.Bundle` for `executor.UseSurface(...)`. Go-side proxies are generated when the source interface is marked with `// ffigen:proxy`.

For examples and runtime integration details, see [DOCS.md](./DOCS.md).

## Development

```bash
make lint test examples
```

Useful focused checks:

```bash
GOCACHE=/tmp/go-build-cache bash -lc 'cd core && go test -timeout 180s ./runtime ./runtime/tests'
GOCACHE=/tmp/go-build-cache bash -lc 'cd ffilib && go test -timeout 180s ./...'
timeout 180s env GOCACHE=/tmp/go-build-cache make coverage
```

## Documentation

- [DOCS.md](./DOCS.md)
- [LSP.md](./LSP.md)
- [TODO.md](./TODO.md)

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
