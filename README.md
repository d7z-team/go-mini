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

## Task Concurrency

Go-Mini uses a single-threaded cooperative fiber model:

- `go f()` schedules a VM fiber; it does not return a handle or result
- the VM never runs two fibers in parallel; switching only happens at VM safe points
- `task.Yield()` gives another runnable fiber a chance to run
- `task.Sleep(ms)` parks the current fiber and lets the scheduler run other fibers
- captured closures share normal VM state with the parent because there is no parallel execution

Lifecycle rules:

- root `main` returning stops all unfinished child fibers immediately
- unfinished background fibers are not awaited automatically
- a child fiber panic fails the whole VM execution unless recovered inside that fiber
- removed APIs are intentionally unavailable: `spawn`, `await`, task handles, task groups, task status, and task cancellation

## Docs

- [DOCS.md](./DOCS.md)
- [LSP.md](./LSP.md)
- [AGENTS.md](./AGENTS.md)
- [TODO.md](./TODO.md)

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
