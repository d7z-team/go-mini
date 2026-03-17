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
6.  **[x] 错误处理协议重构 (Generic Result<T>)**
    *   **[x] 类型系统支持**：在 `ast.GoMiniType` 中实现 `Result<T>` 泛型解析逻辑。
    *   **[x] 运行时变量支持**：在 `runtime.Var` 中实现 `TypeResult` 专用存储与访问逻辑。
    *   **[x] FFI 协议升级**：实现 `[Status][Payload]` 序列化协议，升级 `evalFFI` 自动装箱。
    *   **[x] 生成器同步**：升级 `ffigen` 以支持生成基于 `Result<T>` 的 Router 与 Proxy.
    *   **[x] 动态容器支持**：实现 `TypeMap` 的完整 FFI 序列化，支持非结构体的纯 Map 传输。
7.  **[ ] 标准库全量生成与迁移 (Migration)**
    *   **[x] 核心库迁移**：将 `os`, `fmt`, `io` 等接口全面迁移至 `Result<T>` 契约。
    *   **[x] 关键库注入**：实现 `json`, `time` 库的 FFI 封装。
    *   **[x] 运行安全增强**：在解释器循环中增加对 `context.Context` 取消信号的感知。
    *   **[x] 补充复杂场景测试**：已通过 robustness_test 验证字段排序、Any 包装及 Nil 比较。
    *   **[ ] 网络库注入**：实现 `net` 库的 FFI 封装（后续处理）。
    *   **[x] 类型映射增强**：完善 `TypeMap` 的动态字段访问与边界鲁棒性（包含深度赋值与切片支持）。

---

## 七、 未来特性演进 (Future Roadmap)

在完善了基础架构和 FFI 隔离协议后，为了使 `go-mini` 成为一个对标 Lua、JS 等成熟引擎的现代通用脚本系统，需规划以下核心功能的研发：

1.  **[ ] 高级控制流与函数式编程**
    *   **[ ] 匿名函数与闭包 (Closures)**：支持 `func() { ... }` 表达式，并实现变量的逃逸与环境捕获。这是实现异步回调、事件监听的关键。
    *   **[x] 变长参数定义 (Variadic Functions)**：支持脚本内部使用 `func log(args ...any)` 进行二次封装，提升脚本的工程能力。
        *   *评估*: **难度: 低~中**。只需在调用时将末尾参数打包为 `VMArray`。
        *   *多语言兼容性: 极高*。JS (`...args`), Python (`*args`), Java (`Object...`), Go (`...any`) 均在语义上完美契合“末尾参数数组化”的底层抽象。
2.  **[ ] 健壮性与异常处理**
    *   **[-] 脚本级错误恢复 (`recover` / Try-Catch) (决定不实现)**：支持在脚本层级截获异常，防止局部异常导致整个引擎/任务崩溃，增强脚本容错性。
        *   *评估*: **难度: 高**。需要干预执行器的控制流，实现虚拟机级别的异常展开 (Exception Unwinding)。
        *   *多语言兼容性: 存在语义鸿沟*。JS/Python/Java 采用结构化的 `try-catch`，而 Go 采用延迟栈展开的 `defer-recover`。底层引擎建议统一采用 `TryCatchStmt` 原语（对 JS/Python 更友好），但这会导致 Go 前端的自动转换面临巨大挑战，可能需要引入特定宏或妥协性语法。
        *   *替代方案评估*: **`Result<T>` 模式能否完全替代 `try/catch/recover`？**
            *   **可以替代“业务级错误 (Expected Errors)”**：对于文件找不到、网络超时、格式解析失败等预期内的业务异常，`Result<T>` 配合多变量解构（`val, err := f()`）是极其完美和高效的，完全不需要 `try/catch`。它甚至比 `try/catch` 更清晰地强制开发者处理错误。
            *   **无法替代“致命缺陷与越界 (Unexpected Panics/Traps)”**：`Result<T>` 无法捕获脚本自身的**运行时陷阱 (Traps)**。例如：数组越界 (`arr[100]`)、除零错误 (`a / 0`)、向 nil Map 写入数据、或者深度嵌套逻辑中主动触发的 `panic("unreachable")`。这些陷阱目前会直接中断整个虚拟机。
        *   **最终决策 (Final Decision): 坚持 Fail-Fast (快速失败) 哲学**。
            *   作为“逻辑粘合剂”或“短周期任务”的脚本，在遭遇如数组越界等严重代码缺陷时，直接崩溃 (`panic`) 交由宿主处理是最安全的做法，能有效防止脏数据污染。
            *   保持引擎的极度轻量与高性能，拒绝引入复杂的虚拟机异常控制流开销。
            *   因此，我们**不会在当前引擎级别提供内部错误拦截机制**。
    3.  **[x] 模块化与工程化管理**
    *   **[x] 真正的包加载系统 (Module Import)**：提供一个抽象的 Module Loader，支持跨文件调用（例如 `import "utils"` 加载纯脚本文件），解决单文件代码臃肿的问题。
    *   **[x] 内存预分配 (`make`)**：支持使用 `make([]int, 0, 100)` 或预分配容量的 Map，优化大量数据处理时的 GC 压力。
