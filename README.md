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
GOCACHE=/tmp/go-build-cache go test -timeout 180s ./core/runtime
GOCACHE=/tmp/go-build-cache go test -timeout 180s ./core/e2e/...
GOCACHE=/tmp/go-build-cache go test -timeout 180s ./...
```

## VM Value And Reference Model

Go-Mini stores variables in VM slots. A slot owns the declared type, and assignment normalizes the incoming value into that slot.

- Primitive values, bytes, and VM structs use value semantics.
- VM structs are represented as structs, not maps; their fields are typed slots.
- Arrays, maps, VM pointers, closures, modules, interfaces, and host handles are reference values.
- `Ptr<T>` is a controlled VM slot reference, not a host memory address.
- Passing a struct to a function or value receiver copies the struct value; mutating a pointer/reference field still mutates the referenced object.

## FFI Host Objects

FFI struct schemas are either `VMValue` or `HostOpaque`.

- `VMValue` structs can be created and copied by VM code.
- `HostOpaque` structs are opaque host resources and are only visible as `HostRef<T>`.
- VM code cannot create opaque host objects with `T{}`, `var x T`, or `new(T)`.
- Opaque host objects must come from FFI factories or FFI return values, for example `sync.NewWaitGroup()`.

## Fiber Concurrency

Go-Mini uses a single-threaded cooperative fiber model:

- `go f()` schedules a VM fiber; it does not return a handle or result
- the VM never runs two fibers in parallel; switching only happens at VM safe points
- VM code does not expose a public yield API
- synchronous FFI calls still block the VM; only async FFI returns create suspend/resume points
- captured closures share normal VM state with the parent because there is no parallel execution

Lifecycle rules:

- root `main` returning stops all unfinished child fibers immediately
- unfinished background fibers are not waited for automatically
- a child fiber panic fails the whole VM execution unless recovered inside that fiber

## Docs

- [DOCS.md](./DOCS.md)
- [LSP.md](./LSP.md)
- [AGENTS.md](./AGENTS.md)
- [TODO.md](./TODO.md)

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
