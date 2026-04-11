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

Go-Mini exposes VM-native task primitives plus a `task` module facade:

- `spawn(fn, ...args)` creates a task and returns `Ptr<task.Task>`
- `await(task)` waits and returns the result, but rethrows task failure/cancel as runtime error
- `go f()` is Go syntax sugar for fire-and-forget spawn
- `task.NewTaskGroup()` creates a task-aware group for `Ptr<task.Task>`
- `task.AddTask/WaitTasks/GroupErr/CancelGroup` manage task collections
- `task.Status(task)` returns `pending|running|succeeded|failed|canceled`
- `task.Err(task)` returns `nil` for pending/running/succeeded tasks, and `Error` for failed or canceled tasks
- `task.Cancel(task)` requests cancellation through task context
- captured closures use task-boundary snapshot semantics: child tasks see a copy of captured VM values, and child writes do not flow back to the parent
- captured host handles and task handles keep shared identity across task boundaries, so child tasks can still call host methods or await/query existing tasks

Lifecycle rules:

- root `main` returning cancels all unfinished child tasks
- shutdown cancellation is best-effort and observed only at VM safe points
- unfinished background tasks are not awaited automatically
- `go` task failures do not interrupt the parent flow unless explicitly observed via `await` or `task.Err`
- snapshot capture still rejects VM pointers, modules, runtime-backed interfaces, recursive containers, and other task-unsafe runtime objects

## Docs

- [DOCS.md](./DOCS.md)
- [LSP.md](./LSP.md)
- [AGENTS.md](./AGENTS.md)
- [TODO.md](./TODO.md)

## License

This project is licensed under the MIT License. See [LICENSE](./LICENSE).
