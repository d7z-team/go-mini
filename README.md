# Go-Mini

Go-Mini is a Go-like scripting engine with a bytecode-first execution model.

The architecture is split into clear layers:

- Frontend: Go AST -> Mini AST conversion, semantic validation, and LSP queries
- Compiler: stable `go-mini-bytecode` artifacts and executable `PreparedProgram`
- Runtime: executes lowered task plans instead of high-level AST
- FFI: schema-only bridge generation via `cmd/ffigen`

## Highlights

- `bytecode-first`
  Serialization, loading, and disassembly all use `go-mini-bytecode`.
- `no-AST runtime`
  The non-debug execution path consumes prepared plans and bytecode.
- `schema-only FFI`
  Runtime FFI registration is based on parsed schemas.
- `short VM type names`
  `ffigen:module` defines VM-facing type namespaces such as `order.Order` and `io.File`.

## Key Directories

```text
cmd/
  exec/      # bytecode-first CLI: compile, disassemble, execute
  ffigen/    # FFI code generator
core/
  ast/       # AST, semantics, LSP queries
  compiler/  # bytecode / executable blueprint compilation
  runtime/   # VM runtime consuming prepared plans
  ffilib/    # standard library FFI modules
  e2e/       # language and module end-to-end tests
```

## Quick Start

### Execute Script From Go

```go
executor := engine.NewMiniExecutor()
executor.InjectStandardLibraries()

program, err := executor.NewRuntimeByGoCode(`
package main
func main() {
    println("hello")
}
`)
if err != nil {
    panic(err)
}
if err := program.Execute(context.Background()); err != nil {
    panic(err)
}
```

### Export Bytecode

```go
executor := engine.NewMiniExecutor()
compiled, err := executor.CompileGoCode(`package main`)
if err != nil {
    panic(err)
}
payload, err := compiled.MarshalBytecodeJSON()
if err != nil {
    panic(err)
}
_ = payload
```

### CLI

```bash
# run source
mini-exec -run script.mini

# compile to bytecode JSON
mini-exec -o script.json script.mini

# disassemble
mini-exec -d script.mini

# execute from bytecode
mini-exec -bytecode script.json
```

## FFI Generation

`ffigen` uses a minimal CLI:

```bash
go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
```

Naming rules:

- VM-visible namespaces come from `ffigen:module`
- Local types use short names such as `order.Order`
- Cross-package FFI types resolve to the imported module namespace, such as `io.File`
- Full Go import paths stay internal and are not exposed in VM schema text

See [DOCS.md](./DOCS.md), [LSP.md](./LSP.md), and [AGENTS.md](./AGENTS.md) for details.
