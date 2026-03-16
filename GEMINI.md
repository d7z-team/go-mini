# Go-Mini AI 开发指南与项目结构

欢迎来到 `go-mini` 项目！本文件 (`GEMINI.md`) 是任何参与本代码库开发的 AI Agent 或开发者的核心指令。它概述了核心架构约束、项目布局以及必须遵循的严格规则，以维护这个**高性能、绝对隔离的脚本执行器**的完整性。

---

## 🏗️ 1. 项目结构

项目被划分为独立、松耦合的层级。在进行任何修改前，理解这种拓扑结构至关重要。

```text
go-mini/
├── cmd/
│   └── ffigen/         # 编译期 FFI 包装生成器。生成 Proxy/Router 代码。
├── core/
│   ├── ast/            # AST 定义、语义验证器和返回分析器。
│   │   ├── ast_stmt.go / ast_expr.go   # 核心 AST 节点（严格的封闭集合）。
│   │   ├── ast_valid.go                # 静态语义验证。
│   │   └── ast_types.go                # GoMiniType 系统（封闭的原语类型）。
│   ├── e2e/            # 端到端测试。所有新功能必须在此处有对应的测试。
│   ├── ffigo/          # FFI IPC 协议 & Go 源码到 AST 转换器。
│   │   ├── bridge.go / buffer.go       # 零拷贝二进制序列化协议。
│   │   └── converter.go                # 将标准 Go AST 转换为 Mini-AST。
│   ├── ffilib/         # 标准库 FFI 实现。
│   │   └── oslib, fmtlib, jsonlib...   # 宿主侧资源实现。
│   └── runtime/        # 虚拟机 (执行器)。
│       ├── executor.go                 # 核心评估循环。
│       ├── scope.go                    # 内存隔离、调用栈和 Var 类型。
│       └── ffi.go                      # FFI 动态路由。
├── core/executor.go    # 顶层 API (MiniExecutor, MiniProgram)。
└── core/parser.go      # JSON 到 AST 的反序列化逻辑。
```

---

## 🛡️ 2. 核心架构铁律 (The "Iron Laws")

在修改 `go-mini` 时，你**必须**遵守以下原则。违反这些原则将破坏引擎基础的安全性和性能保证。

### I. 绝对内存隔离 (Absolute Memory Isolation)
*   **规则**: VM (Runtime) 和宿主 (Go 环境) **绝不能**共享原生内存指针 (除了在 FFI 调用期间只读的零拷贝 `[]byte` 缓冲区)。
*   **实现**: 复杂的宿主对象 (如 `os.File`, `net.Conn`) 通过**句柄系统 (Handle System)** 管理。VM 内部永远只持有 `uint32` 的句柄 ID (`TypeHandle`)。
*   **安全**: 脚本无法对句柄进行算术运算。`Executor` 在任何操作前会严格验证 `VType`。

### II. 零反射 (Zero Reflection)
*   **规则**: 执行路径 (`core/runtime` 和 `core/ast`) 中**禁止**使用 `reflect` 包。
*   **实现**: 所有动态 FFI 调用都通过静态生成的代码 (`ffigen`) 路由，使用整数 `MethodID` 和字节数组载荷 (payload)。

### III. 封闭类型系统 (Closed Type System / Data Reduction)
*   **规则**: 不要试图将每种 Go 类型都映射到 VM 中。
*   **实现**: VM 只理解原语：`TypeInt` (int64), `TypeFloat` (float64), `TypeString`, `TypeBool`, `TypeBytes` ([]byte), `TypeArray`, `TypeMap`, `TypeResult`, `TypeHandle`, 以及 `TypeAny`。
*   **转换**: `GoToASTConverter` 在执行前负责 Go 类型到 VM 类型的“降维” (例如 `int32` -> `Int64`)。

### IV. 泛型错误协议 (`Result<T>`)
*   **规则**: 可能失败的 FFI 函数必须返回 `Result` 包装器。
*   **实现**: VM 原生理解 `TypeResult`。访问 Result 对象的 `.val` 或 `.err` 是一项内置的 VM 功能，而不是结构体字段访问。FFI 响应使用 `[StatusByte][Payload]` 二进制格式。

---

## 🤖 3. AI 贡献者工作流指南

当你 (AI Agent) 被要求在 `go-mini` 中实现功能、修复 Bug 或重构代码时，请遵循以下严格的工作流：

### A. 执行引擎修改 (`core/runtime`)
1.  **类型安全第一**: 在添加新的操作符或内置函数时，**始终**使用 `runtime.Var` 上的安全访问器 (`v.ToInt()`, `v.ToFloat()`, `v.ToBool()`, `v.ToBytes()`)。在没有断言 `v.VType == TypeInt` 的情况下，绝不直接读取 `v.I64`。
2.  **资源限制**: 每一个新的 AST `Stmt` 评估循环都必须遵守 `StepLimit` (指令计数器)，并检查 `ctx.Context.Err()`，以防止 CPU 耗尽和死循环。
3.  **Defer & Panic**: 确保任何新的执行作用域在退出时都正确调用了 `ctx.Stack.RunDefers()`，以防止宿主机文件描述符 (FD) 泄漏。

### B. AST & 解析器修改 (`core/ast` & `core/parser.go`)
1.  **对称性**: 如果你添加了一个新的 AST 节点 (例如 `NewStmt`)，你必须同步更新：
    *   `core/ast/ast_stmt.go` (结构体定义、`Check()`, `Optimize()`)。
    *   `core/ffigo/converter.go` (将 Go AST 映射为 Mini AST)。
    *   `core/parser.go` (`unmarshalNodeData` JSON 反序列化)。
    *   `core/runtime/executor.go` (执行逻辑)。
2.  **没有“幽灵”节点**: 不要将 `todo: expr %T` 或 `panic("not implemented")` 遗留在执行器中。如果一个 AST 节点存在，它要么必须是完全可执行的，要么必须被验证器严格阻拦。

### C. 添加新的标准库 (`core/ffilib`)
1.  创建目录 (例如 `core/ffilib/netlib`)。
2.  在 `interface.go` 中定义接口。对于有状态的对象 (它们将成为 Handles)，使用指针接收者，并严格使用标准的 Go 类型。
3.  在 `host.go` 中实现宿主逻辑。
4.  运行 `go run ./cmd/ffigen` 生成 `_ffigen.go` 路由代码。
5.  在 `core/executor.go` (`InjectStandardLibraries`) 中将新库注入执行器。

### D. 测试 (`core/e2e`)
1.  **强制要求**: 每个 Bug 修复或功能实现都必须在 `core/e2e/` 中附带一个测试。
2.  **鲁棒性**: 在测试 VM 逻辑时，编写实际的 Go 脚本字符串，通过 `NewRuntimeByGoCode` 编译并执行它们。除非专门测试解析器，否则不要在测试中手动构造 AST。

---

**记住:** 你正在操作一个安全关键的沙盒引擎。在任何时候，严格的类型验证、资源边界控制和确定性的执行都优先于语法糖。