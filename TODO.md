# Go-Mini 重构计划：完全隔离的 Raw-FFI 架构

## 一、 架构总览与核心原则

本项目旨在将 `go-mini` 重构为一个完全隔离的执行器，主要遵循以下核心原则：

1.  **绝对内存隔离 (Absolute Memory Isolation)**：VM（执行器）与 Host（宿主 Go 环境）之间**不存在任何共享指针**。VM 内的 `runtime.Var` 只能存储基础值类型或 VM 自定义的纯内部结构。
2.  **零反射 (Zero-Reflection)**：彻底移除 `core/runtime/adapter.go` 以及 `reflect.Value.Call`。
3.  **Raw 流通信 (Raw Binary IPC)**：FFI 调用不再传递 Go 对象实例或 JSON，而是通过一片连续的 `[]byte` 进行高效的二进制序列化与反序列化。
4.  **代码生成 (Code Generation)**：所有 FFI 边界代码全部通过 `ffigen` 工具在编译期静态生成。

---

## 二、 核心通信协议设计 (Raw FFI Protocol)

### 1. FFI Bridge 接口定义
在 `core/ffigo/bridge.go` 中定义唯一的通信契约。 [x]

### 2. 内存布局与数据降维 (Data Mapping)
为了消除 Go 类型系统的复杂性，所有跨界数据必须降维： [x]
*   **标量降维 (Scalar)**: 
    *   `int`, `uint`, `int32` 等统一映射为 VM 的 `TypeInt (int64)`。 [x]
    *   `float32`, `float64` 统一映射为 VM 的 `TypeFloat (float64)`。 [x]
*   **容器缓冲区化 (Bufferized Containers)**:
    *   `[]byte` (VM 内部类型 `TypeBytes`) 统一作为 **Raw Buffer** 处理。 [x]
    *   在 FFI 调用时，VM 侧将 `TypeArray` ([]*Var) 显式序列化为 `ffigo.Buffer` 字节流。 [x]
    *   宿主侧通过 `ffigo.Reader` 将字节流还原为 Go Slice，处理后以相同方式返回。 [x]
    *   **零拷贝 (Zero-copy)**: 对于 `TypeBytes`，`ffigo.Reader.ReadBytes()` 直接引用原始传输缓冲区，避免内存分配。 [x]

---

## 三、 资源管理：句柄机制 (Handle System)

对于不能跨界的值（如 `os.File`, `net.Conn`），采用**句柄（Handle）隔离映射**。 [x]

1.  **Host 端维护注册表**：宿主机持有一个 `map[uint32]interface{}`。 [x]
2.  **VM 端持有 ID**：VM 仅存储一个 `uint32` 的 Handle ID。 [x]
3.  **I/O 调用高效化**：宿主端根据 ID 查找真实资源并执行操作，数据通过 Raw Buffer 交换。 [x]

---

## 四、 FFI 自动化与源码适配

### 1. 自动包装生成器 (ffigen)
新增 `cmd/ffigen` 工具。开发者手写 Go interface 声明，工具将自动生成 **Host Router** 和 **VM Proxy**。 [x]

### 2. Go 源码到隔离 AST 转换 (GoToASTConverter)
实现了 `core/ffigo/converter.go`，支持将标准 Go 源码直接转换为适配隔离架构的 AST： [x]
*   **语法适配**: 自动将 `:=` 展开为声明与赋值，处理嵌套作用域 (`Inner: true`)。 [x]
*   **调用标准化**: 统一将标识符调用（如 `println`）转换为 `ConstRefExpr`，便于执行器进行静态 FFI 路由。 [x]
*   **类型预规约**: 转换阶段即完成 Go 类型到 VM 原语（`Int64`, `Float64` 等）的初步规约。 [x]

---

## 五、 核心代码重构执行点

### 1. 移除反射逻辑 (The Purge)
*   **[x] 删除 `core/runtime/adapter.go`**。
*   **[x] 清理 `core/runtime/executor.go`**。

### 2. 重构 VM 类型系统 (Closed Type System)
修改 `runtime.Var` 确保其只持有隔离的基础类型。 [x]
同时，**废弃 `core/ast/std` 中的 1:1 Go 对象映射**：
*   **[x] AST 瘦身**: 移除 `ast_std_int.go` 等文件中的复杂方法集定义。
*   **[x] 原语化**: 验证器将所有 Go 标量类型规约为 VM 原语 (`Int64`, `Float64` 等)，仅支持算术运算符，不支持方法调用。
*   **[x] 内置函数**: 原语的方法（如 `int.String()`）改为 VM 内置函数（如 `itoa()`）或显式 FFI 调用。

---

## 六、 落地执行计划 (Action Plan)

1.  **[x] 建立基建 (Foundation)**
2.  **[x] 清洗旧时代 (Purge)**
3.  **[x] 重写基本类型系统 (Type System)**
4.  **[x] 完善执行器隔离逻辑 (Isolation Logic)**
    *   **[x] 修复递归上下文与空指针**：确保 `WithFuncScope` 和 `Inner Block` 正确透传。
    *   **[x] 实现 FFI 路由分发**：支持 `RegisterFFI` 动态绑定外部 Bridge。
    *   **[x] 数据序列化/反序列化**：在执行引擎内实现 `evalFFI` 字节流转换。
5.  **[x] 源码适配转换器 (Converter)**：实现 `GoToASTConverter` 跑通 Go 源码测试。
6.  **[x] 错误处理协议重构 (Generic Tuple<T, String>)**
    *   **[x] 类型系统支持**：在 `ast.GoMiniType` 中实现 `Tuple<T...>` 泛型解析逻辑。
    *   **[x] 运行时变量支持**：在 `runtime.Var` 中实现多返回值解构逻辑。
    *   **[x] FFI 协议升级**：实现紧凑序列化协议，升级 `evalFFI` 自动打包 Tuple。
    *   **[x] 生成器同步**：升级 `ffigen` 以支持生成基于 `Tuple` 的 Router 与 Proxy.
    *   **[x] 动态容器支持**：实现 `TypeMap` 的完整 FFI 序列化，支持非结构体的纯 Map 传输。
7.  **标准库全量生成与迁移 (Migration)**
    *   **[x] 核心库迁移**：将 `os`, `fmt`, `io` 等接口全面迁移至 `Tuple<T, String>` 契约。 [x]
    *   **[x] 关键库注入**：实现 `json`, `time` 库的 FFI 封装。 [x]
    *   **[x] 运行安全增强**：在解释器循环中增加对 `context.Context` 取消信号的感知。 [x]
    *   **[x] 补充复杂场景测试**：已通过 robustness_test 验证字段排序、Any 包装及 Nil 比较。 [x]
    *   **[x] FFI 句柄 OOP 转换**：实现了 `f.Write` 风格的方法调用映射。 [x]
    *   **[ ] 网络库注入**：实现 `net` 库的 FFI 封装（后续处理）。
    *   **[x] 类型映射增强**：完善 `TypeMap` 的动态字段访问与边界鲁棒性（包含深度赋值与切片支持）。 [x]


---

## 七、 未来特性演进 (Future Roadmap)

在完善了基础架构和 FFI 隔离协议后，为了使 `go-mini` 成为一个对标 Lua、JS 等成熟引擎的现代通用脚本系统，需规划以下核心功能的研发：

1.  **[ ] 高级控制流与函数式编程**
    *   **[x] 匿名函数与闭包 (Closures)**：支持 `func() { ... }` 表达式，并实现变量的逃逸与环境捕获。采用按需 Cell 装箱技术，兼顾了极致性能与完备性。
    *   **[x] 变长参数定义 (Variadic Functions)**：支持脚本内部使用 `func log(args ...any)` 进行二次封装，提升脚本的工程能力。
        *   *评估*: **难度: 低~中**。只需在调用时将末尾参数打包为 `VMArray`。
        *   *多语言兼容性: 极高*。JS (`...args`), Python (`*args`), Java (`Object...`), Go (`...any`) 均在语义上完美契合“末尾参数数组化”的底层抽象。
2.  **[ ] 健壮性与异常处理**
    *   **[x] 脚本级错误恢复 (`recover` / Try-Catch)**：支持在脚本层级截获异常，防止局部异常导致整个引擎/任务崩溃，增强脚本容错性。
        *   *评估*: **难度: 中**。通过引入 `PanicError` 冒泡机制和 `TryStmt` AST 原语，可以同时兼容 Go 的 `defer-recover` 动态异常模型和 JS/Java 的 `try-catch` 词法异常模型。
        *   *实施路径*:
            - [x] `ast_stmt.go`: 新增 `TryStmt` 节点。
            - [x] `runtime/scope.go`: 扩展 `Stack` 以支持 `PanicVar` 寄存器。
            - [x] `runtime/executor.go`: 实现 `PanicError` 冒泡逻辑，改造 `panic` 注入函数。
            - [x] `runtime/executor.go`: 实现 `recover` 内建函数。
            - [x] `runtime/executor.go`: 实现 `TryStmt` 执行逻辑。
            - [x] `ffigo/converter.go`: 修复了 IfStmt 的 Init 转换，支持了 recover() 的语义路径。
    3.  **[x] 模块化与工程化管理**
    *   **[x] 真正的包加载系统 (Module Import)**：提供一个抽象特 Module Loader，支持跨文件调用（例如 `import "utils"` 加载纯脚本文件），解决单文件代码臃肿的问题。
    *   **[x] 内存预分配 (`make`)**：支持使用 `make([]int, 0, 100)` 或预分配容量的 Map，优化大量数据处理时的 GC 压力。
    *   **[x] 纯表达式执行 (`Eval`)**：支持计算单个 Go 表达式字符串，无需 package 和 main 声明，适用于规则引擎。

    ---

    ## 十、 重构计划：纯表达式执行支持 (Expression Evaluation) [x]
    目标：支持计算独立的 Go 表达式，复用 FFI 路由和无状态执行器。

    ### Phase 1: 转换器增强 [x]
    - [x] `converter.go`: 实现 `ConvertExprSource` 方法，调用 `go/parser.ParseExpr`。

    ### Phase 2: API 封装 [x]
    - [x] `executor.go`: 在 `MiniExecutor` 中提供 `Eval` 接口，自动处理环境注入与结果返回。

    ### Phase 3: 验证与测试 [x]
    - [x] `eval_test.go`: 验证算术、逻辑及带环境变量的表达式计算。

---

## 十一、 API 易用性与深度隔离加固 [x]
目标：提供“开箱即用”的宿主交互体验，同时补全最后的内存安全盲区。

### Phase 1: 自动化数据规范化 (Normalization) [x]
- [x] `engine/executor.go`: 实现 `normalizeValue` 函数，利用反射自动将宿主 `struct` 映射为 VM `TypeMap`。
- [x] `engine/executor.go`: 注入环境变量前自动执行规范化，并对不支持类型返回明确错误。

### Phase 2: 极致内存隔离 (Deep Copy) [x]
- [x] `runtime/executor.go`: 在 `ToVar` 中对 `[]byte` 执行物理克隆 (`copy`)，彻底杜绝切片底层数组共享导致的逃逸。
- [x] `runtime/scope.go`: 实现 `Var.Interface()` 方法，递归地将 VM 内部变量还原为 Go 原生接口类型。

### Phase 3: 规范化类型体系 (Canonical Typing) [x]
- [x] `ast/ast_types.go`: 严格限制类型匹配，仅允许首字母大写的规范类型（`Int64`, `Float64`, `TypeBytes`）。
- [x] `ffigen`: 重构代码生成器，强制输出规范化 `Spec` 签名，修复浮点数零值序列化问题。

### Phase 4: 资源清理现代化 (Go 1.24 Cleanup) [x]
- [x] `runtime/scope.go`: 迁移 Handle 自动回收逻辑至 `runtime.AddCleanup`，消除循环引用泄漏风险并提升 GC 效率。

---

## 十二、 语义完备性与高并发安全加固 (Current Focus) [x]
目标：在维持无状态隔离架构的前提下，提供更接近 Go 语法的开发体验，并保障生产级并发稳定性。

### Phase 1: 引用语义与 `new(T)` 深度初始化 [x]
- [x] `runtime/executor.go`: 实现了 `initializeType` 递归函数，支持 `new(Struct)` 时的深度零值展开。
- [x] `runtime/executor.go`: 为递归初始化增加了 **10 层深度限制**，防御恶意 AST 导致的 Stack Overflow。
- [x] `ast/ast_expr.go`: 加固了 `make` 和 `new` 的编译期校验，禁止对结构体执行 `make`。

### Phase 2: 高并发安全保障 [x]
- [x] `engine/executor.go`: 为 `MiniExecutor` 引入 `sync.RWMutex`。
- [x] `engine/executor.go`: 在 `RegisterFFI`、`RegisterModule` 及所有读路径（`Eval`, `Execute`）中实现了完整的读写锁保护。

### Phase 3: 极致鲁棒性与资源安全 [x]
- [x] `runtime/executor.go`: 统一了 `isEmptyVar` 助手函数，实现了 Nil 安全的比较运算，消除了空字符串歧义。
- [x] `engine/executor.go`: 在 Snippet 模式（`Execute`）边界引入了 **Handle 生命周期强制清理**，防止 FD 泄露。
- [x] `runtime/executor.go`: 实现了 Nil 容器（Map/Array）访问的自动零值返回，对齐 Go 原生语义。

---

## 十三、 后续演进计划 [ ]
1.  **[ ] 网络库注入**：实现 `net` 库的 FFI 封装。
2.  **[ ] 性能调优**：针对常用的 Map 成员访问进行指令级缓存优化。
3.  **[ ] 多平台分发验证**：测试编译后的 JSON AST 在跨架构（ARM/WASM）环境下的执行一致性。
