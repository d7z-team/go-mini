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

在修改 `go-mini` 时，你**必须**遵守以下原则。违反 these 原则将破坏引擎基础的安全性和性能保证。

### I. 绝对内存隔离 (Absolute Memory Isolation)
*   **规则**: VM (Runtime) 和宿主 (Go 环境) **绝不能**共享原生内存指针 (除了在 FFI 调用期间只读的零拷贝 `[]byte` 缓冲区)。
*   **零拷贝安全约束**: FFI 宿主侧实现**严禁**在 `Call` 或 `Invoke` 方法返回后继续持有或访问 `args` 字节数组的引用。任何异步处理必须在返回前完成数据的深拷贝。
*   **实现**: 

    *   **句柄系统 (Handle System)**: 复杂的宿主对象 (如 `os.File`, `net.Conn`) 通过句柄管理。VM 内部永远只持有 `uint32` 的句柄 ID (`TypeHandle`)。
    *   **沙盒指针 (Sandbox Pointers)**: 脚本层支持 `Ptr<T>` 类型和 `*p` (StarExpr) 运算符。这些指针在 VM 内部映射为 Handle 或隔离容器，解引用操作由引擎拦截并返回数据的受控副本，绝不外泄原生地址。
*   **安全**: 脚本无法对句柄进行算术运算。`Executor` 在任何操作前会严格验证 `VType`。

### II. 零反射 (Zero Reflection)
*   **规则**: 执行路径 (`core/runtime` 和 `core/ast`) 中**禁止**使用 `reflect` 包。
*   **实现**: 所有动态 FFI 调用都通过静态生成的代码 (`ffigen`) 路由，使用整数 `MethodID` 和字节数组载荷 (payload)。

### III. 封闭类型系统 (Closed Type System / Data Reduction)
*   **规则**: 不要试图将每种 Go 类型都映射到 VM 中。
*   **实现**: VM 只理解原语：`TypeInt` (int64), `TypeFloat` (float64), `TypeString`, `TypeBool`, `TypeBytes` ([]byte), `TypeArray`, `TypeMap`, `TypeHandle`, 以及 `TypeAny`。
*   **引用语义约定**: 所有的复合类型（Array, Map, 以及由 Map 模拟的 Struct）在 VM 内部传递时均采用**引用语义**。赋值操作或方法调用不会触发深度拷贝。
*   **转换**: `GoToASTConverter` 在执行前负责 Go 类型到 VM 类型的“降维” (例如 `int32` -> `Int64`)。

### IV. 泛型错误协议 (`Tuple<T, String>`)
*   **规则**: 可能失败的 FFI 函数返回 `Tuple`，其中最后一个元素为 `String` 类型的错误信息。
*   **实现**: 引擎使用多返回值解构来处理 FFI 错误。Host 侧如果发生严重故障或 Panic，可以触发脚本级的原生 Panic。

### V. 语言中立与规范化表达 (Language Neutrality & Canonical Representation)
*   **规则**: 核心引擎 (`core/ast`, `core/runtime`) 必须保持语言中立。禁止在底层类型判断逻辑中加入特定前端语言的语法兼容（如 Go 的 `[]T` 或 `map[K]V`）。
*   **实现**:
    *   **规范化**: 引擎只识别其定义的规范化字符串表达，如 `Array<T>`、`Map<K, V>`、`Ptr<T>` 和 `Tuple<T...>`。所有类型前缀必须首字母大写。
    *   **职责上移**: 任何前端特有的语法糖转换（Normalization）必须在转换层完成（例如在 `core/ffigo/converter.go` 中将 Go风格类型映射为规范格式）。
    *   **零容错**: 执行器在进行 FFI 序列化或类型断言时，应严格匹配规范格式，不应引入 `strings.ToLower` 等宽容性处理以牺牲性能或破坏严谨性。

### VI. 无状态执行与并发安全 (Stateless Execution & Concurrency Safety)
*   **规则**: `Executor` 必须是绝对无状态的只读蓝图。严禁在 `Executor` 结构体中添加任何特定于单次执行的运行时状态。
*   **实现**: 所有单次执行的可变状态（如指令计数器 `stepCount`、活跃句柄列表 `activeHandles`、模块缓存 `moduleCache` 等）必须下沉并封装到 `StackContext` (Session) 中。每次调用 `Execute` 都必须在本地栈上创建一个全新的、相互隔离的 `StackContext`，以确保宿主层多协程并发调用的绝对线程安全。
*   **并发保护**: `MiniExecutor`（宿主层容器）在动态注册模块或 FFI 路由时，必须持有 `sync.RWMutex` 以防止并发 Map 读写崩溃。

### VII. AST JSON 双向对称性 (AST JSON Symmetry)
*   **规则**: 所有的 AST 节点必须 100% 支持 JSON 序列化与反序列化。禁止存在只能从 Go 源码转换而来，却无法从 JSON 恢复的“幽灵”节点。
*   **实现**: 当在 `core/ast` 目录下新增或修改任何实现了 `Node`、`Expr` 或 `Stmt` 接口的结构体时，不仅要确保其包含正确的 `json` struct tags，还**必须强制在 `core/parser.go` 的 `unmarshalNodeData` 或 `parseExpr` 的 `switch` 分支中实现对应的反序列化逻辑**。这是引擎能够支持跨进程“物理级编译与执行分离”的基石。

### VIII. 资源生命周期自动回收 (Automatic Handle Cleanup)
*   **规则**: 任何执行会话产生的宿主句柄（Handle）必须有明确的生命周期终点。
*   **追踪**: `StackContext` 必须持有 `*HandleTracker` (引用计数或共享追踪器)，确保跨模块调用和闭包中产生的句柄能被根 Session 统一感知。
*   **兜底**: `Execute` 和 `Eval` 接口必须在 `defer` 中显式清理 `ActiveHandles` 并运行 `RunDefers()`，防止因脚本异常导致的系统资源（如文件描述符 FD）泄露。

### IX. 递归初始化防御 (Recursive Initialization Defense)
*   **规则**: 运行时进行类型分配（如 `new`）时，递归深度严禁超过 10 层，以防止恶意的循环结构体定义导致宿主 Stack Overflow。

### X. LSP 支撑与语义完整性 (LSP & Semantic Integrity)
*   **非阻塞校验**: `Check(ctx *SemanticContext)` 方法严禁使用“一错即死”模式。遇到错误必须通过 `ctx.AddErrorf` 记录并**强制继续**校验后续分支，以确保 IDE 能一次性展示全量诊断信息。
*   **上下文指针**: `SemanticContext` 必须作为指针传递。严禁对其执行结构体值拷贝，以防止父子作用域间的符号表同步失效。
*   **坐标精度**: 所有 AST 节点的 `Loc` 必须包含 4 坐标（起始行/列、结束行/列）。禁止生成没有坐标或坐标重叠的虚假节点。
*   **缓存按需构建**: `MiniProgram` 中的 `ParentMap` 等 LSP 缓存必须遵循懒加载原则，且必须提供显式的释放接口（如 `ReleaseLSPCache`）。

---

## 🤖 3. AI 贡献者工作流指南

当你 (AI Agent) 被要求在 `go-mini` 中实现功能、修复 Bug 或重构代码时，请遵循以下严格的工作流：

### A. 执行引擎修改 (`core/runtime`)
1.  **类型安全第一**: 在添加新的操作符或内置函数时，**始终**使用 `runtime.Var` 上的安全访问器 (`v.ToInt()`, `v.ToFloat()`, `v.ToBool()`, `v.ToBytes()`)。在没有断言 `v.VType == TypeInt` 的情况下，绝不直接读取 `v.I64`。
2.  **Nil 安全判定**: 在 `evalComparison` 或 `evalIntrinsic` 中添加逻辑时，必须通过全局助手函数 `isEmptyVar` 进行 Nil 安全判定，防止宿主 Panic。
3.  **资源限制**: 每一个新的 AST `Stmt` 评估循环都必须遵守 `StepLimit` (指令计数器)，并检查 `ctx.Context.Err()`，以防止 CPU 耗尽和死循环。
4.  **Defer & Panic**: 确保任何新的执行作用域在退出时都正确调用了 `ctx.Stack.RunDefers()`，以防止宿主机文件描述符 (FD) 泄漏。

### B. AST & 解析器修改 (`core/ast` & `core/parser.go`)
1.  **对称性**: 如果你添加了一个新的 AST node (例如 `NewStmt`)，你必须同步更新：
    *   `core/ast/ast_stmt.go` (结构体定义、`Check()` 必须支持全量报错、`Optimize()`)。
    *   `core/ffigo/converter.go` (将 Go AST 映射为 Mini AST，确保 Loc 坐标提取精准)。
    *   `core/parser.go` (`unmarshalNodeData` JSON 反序列化)。
    *   `core/runtime/executor.go` (执行逻辑)。
    *   `core/ast/query.go` (更新 `Walk` 逻辑，确保新节点可被 LSP 遍历)。
2.  **标识符规范**: AST 中的所有 `Operator` 或 `InterruptType` 必须统一使用 `Ident` 类型（除非该字段纯粹用于 JSON 标识）。

### C. 添加或修改标准库 (`core/ffilib`)
1.  **目录结构**: 创建目录 (例如 `core/ffilib/netlib`)。
2.  **定义接口**: 在 `interface.go` 中定义接口。
    *   使用 `// ffigen:module <name>` 注解接口以定义包名（如 `fmt`, `os`）。
    *   使用 `// ffigen:methods <TypeName>` 注解接口以定义面向对象的方法集（如 `File`），这会自动生成 `__method_TypeName_MethodName` 格式"`"的 FFI 路由。
3.  **生成指令**: 在 `interface.go` 首行添加 `//go:generate` 指令：
    `//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg <pkgname> -out <name>_ffigen.go interface.go`
4.  **宿主实现**: 在 `host.go` 中实现宿主逻辑。
5.  **元数据安全**: Bridge 层**严禁信任** `Var.Type` 字符串，必须基于 `VType` 枚举进行严格的硬断言。
6.  **触发生成**: 运行 `make gen`。
7.  **注入执行器**: 在 `core/executor.go` (`InjectStandardLibraries`) 中使用生成的简洁 API 注入（如 `RegisterOS(o, impl, reg)`）。

### D. 测试 (`core/e2e`)
1.  **强制要求**: 每个 Bug 修复或功能实现都必须在 `core/e2e/` 中附带一个测试。
2.  **鲁棒性**: 在测试 VM 逻辑时，编写实际的 Go 脚本字符串，通过 `NewRuntimeByGoCode` 编译并执行它们。除非专门测试解析器，否则不要在测试中手动构造 AST。

---

**记住:** 你正在操作一个安全关键的沙盒引擎。在任何时候，严格的类型验证、资源边界控制和确定性的执行都优先于语法糖。
