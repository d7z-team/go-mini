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
在 `core/ffigo/bridge.go` 中定义唯一的通信契约。

### 2. 内存布局与数据降维 (Data Mapping)
为了消除 Go 类型系统的复杂性，所有跨界数据必须降维：
*   **标量降维 (Scalar)**: 
    *   `int`, `uint`, `int32` 等统一映射为 VM 的 `TypeInt (int64)`。
    *   `float32`, `float64` 统一映射为 VM 的 `TypeFloat (float64)`。
*   **容器缓冲区化 (Bufferized Containers)**:
    *   `[]byte` (VM 内部类型 `TypeBytes`) 统一作为 **Raw Buffer** 处理。
    *   在 FFI 调用时，VM 侧将 `TypeArray` ([]*Var) 显式序列化为 `ffigo.Buffer` 字节流。
    *   宿主侧通过 `ffigo.Reader` 将字节流还原为 Go Slice，处理后以相同方式返回。
    *   **零拷贝 (Zero-copy)**: 对于 `TypeBytes`，`ffigo.Reader.ReadBytes()` 直接引用原始传输缓冲区，避免内存分配。

---

## 三、 资源管理：句柄机制 (Handle System)

对于不能跨界的值（如 `os.File`, `net.Conn`），采用**句柄（Handle）隔离映射**。

1.  **Host 端维护注册表**：宿主机持有一个 `map[uint32]interface{}`。
2.  **VM 端持有 ID**：VM 仅存储一个 `uint32` 的 Handle ID。
3.  **I/O 调用高效化**：宿主端根据 ID 查找真实资源并执行操作，数据通过 Raw Buffer 交换。

---

## 四、 FFI 自动化与源码适配

### 1. 自动包装生成器 (ffigen)
新增 `cmd/ffigen` 工具。开发者手写 Go interface 声明，工具将自动生成 **Host Router** 和 **VM Proxy**。

### 2. Go 源码到隔离 AST 转换 (GoToASTConverter)
实现了 `core/ffigo/converter.go`，支持将标准 Go 源码直接转换为适配隔离架构的 AST：
*   **语法适配**: 自动将 `:=` 展开为声明与赋值，处理嵌套作用域 (`Inner: true`)。
*   **调用标准化**: 统一将标识符调用（如 `println`）转换为 `ConstRefExpr`，便于执行器进行静态 FFI 路由。
*   **类型预规约**: 转换阶段即完成 Go 类型到 VM 原语（`Int64`, `Float64` 等）的初步规约。

---

## 五、 核心代码重构执行点

### 1. 移除反射逻辑 (The Purge)
*   **[x] 删除 `core/runtime/adapter.go`**。
*   **[x] 清理 `core/runtime/executor.go`**。

### 2. 重构 VM 类型系统 (Closed Type System)
修改 `runtime.Var` 确保其只持有隔离的基础类型。
同时，**废弃 `core/ast/std` 中的 1:1 Go 对象映射**：
*   **[x] AST 瘦身**: 移除 `ast_std_int.go` 等文件中的复杂方法集定义。
*   **[x] 原语化**: 验证器将所有 Go 标量类型规约为 VM 原语 (`Int64`, `Float64` 等)，仅支持算术运算符，不支持方法调用。
*   **[x] 内置函数**: 原本的对象方法（如 `int.String()`）改为 VM 内置函数（如 `itoa()`）或显式 FFI 调用。

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
    * [x] 生成器同步：升级 `ffigen` 以支持生成基于 `Result<T>` 的 Router 与 Proxy.
    * [x] 动态容器支持：实现 `TypeMap` 的完整 FFI 序列化，支持非结构体的纯 Map 传输。
    7.  **[ ] 标准库全量生成与迁移 (Migration)**
    * [x] 核心库迁移：将 `os`, `fmt`, `io` 等接口全面迁移至 `Result<T>` 契约。
    * [ ] 关键库注入：实现 `json`, `time`, `net` 库的 FFI 封装。
    * [x] 运行安全增强：在解释器循环中增加对 `context.Context` 取消信号的感知。
    * [x] 补充复杂场景测试：已通过 robustness_test 验证字段排序、Any 包装及 Nil 比较。
