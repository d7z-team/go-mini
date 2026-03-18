# Go-Mini (Isolated Raw-FFI Edition)

Go-Mini is a high-performance, absolutely isolated Go-like script executor. It is designed for maximum security and I/O efficiency by cutting off direct memory sharing between the host and the VM.

## Key Features

- **Absolute Memory Isolation**: No shared pointers between Host and VM.
- **Zero-Reflection**: The entire execution path (Parser, Validator, Executor) is free of Go's `reflect` package.
- **Raw-FFI IPC**: High-performance binary communication via `ffigo.Buffer` and `ffigo.Bridge`.
- **Thread-Safe Execution**: The `Executor` acts as a stateless blueprint. A single compiled `MiniProgram` can be executed concurrently across thousands of host goroutines without locks or data races.
- **Static Code Generation**: FFI wrappers are generated at compile-time using `cmd/ffigen`.
- **Data Reduction**: All scalar types are mapped to `Int64` or `Float64` for simplicity.
- **Reference Semantics**: Script-defined structs and arrays use **reference semantics** for performance. Assignments and method calls do not trigger a deep copy.

## Architecture

The project is structured around a **Message Passing** architecture:
1.  **VM**: Executes AST nodes, maintains its own stack and heap.
2.  **FFI Bridge**: The only channel for communication, passing `MethodID` and `[]byte`.
3.  **Host**: Receives binary requests, routes them to native Go functions, and returns binary responses.

## Getting Started

Refer to `TODO.md` for current refactoring progress and design details.
