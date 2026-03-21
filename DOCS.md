# Go-Mini 使用文档

`go-mini` 是一个高性能、绝对内存隔离的 Go 语言子集脚本执行引擎。它被设计用于在宿主 Go 程序中安全地执行不受信的脚本，同时通过 Raw-FFI (Foreign Function Interface) 提供极高的 I/O 性能。

## 🌟 核心特性

*   **绝对安全隔离**：脚本运行在完全独立的虚拟机 (VM) 中，与宿主程序零指针共享。
*   **非递归迭代架构**：核心引擎采用迭代式状态机，彻底消除宿主栈溢出风险，支持超深度递归和长循环。
*   **暂停与恢复执行**：原生支持在指令级别暂停脚本执行并随时恢复，适用于长时间运行的任务流控制。
*   **Go 1.22 闭包语义**：完美支持 Go 1.22+ 的循环变量捕获语义，确保闭包行为符合原生 Go 标准。
*   **物理级内存防护**：环境变量中的 `[]byte` 会被自动执行**深度拷贝**，彻底杜绝通过共享切片导致的内存逃逸。
*   **零反射高性能**：底层通信基于编译期生成的路由代码和二进制序列化，彻底摒弃 `reflect` 包，性能极佳。
*   **高并发与线程安全**：执行器采用**无状态蓝图 (Stateless Blueprint)** 设计。同一个编译好的脚本可以在成千上万个宿主 Goroutine 中并发执行，互不干扰。
*   **资源防泄漏**：内置句柄 (Handle) 自动回收机制。即便脚本异常退出，打开的宿主资源也会被强制销毁。

---

## 🚀 快速开始

### 1. 基础执行

```go
package main

import (
	"context"
	"fmt"
	engine "gopkg.d7z.net/go-mini/core"
)

func main() {
	executor := engine.NewMiniExecutor()
	code := `
	package main
	func main() {
		x := 10
		y := 20
	}
	`
	prog, _ := executor.NewRuntimeByGoCode(code)
	_ = prog.Execute(context.Background())
	fmt.Println("脚本执行成功！")
}
```

### 2. 暂停与恢复 (Pause & Resume)

得益于非递归架构，你可以轻松控制脚本的运行节奏。

```go
func main() {
    prog, _ := executor.NewRuntimeByGoCode(longRunningCode)
    
    // 在协程中执行
    go prog.Execute(context.Background())
    
    // 运行一段时间后暂停
    time.Sleep(100 * time.Millisecond)
    session := prog.LastSession()
    if session != nil {
        session.Pause()
        fmt.Println("Execution paused at step:", session.StepCount)
        
        // 稍后恢复
        time.Sleep(1 * time.Second)
        session.Resume()
    }
}
```

### 3. 数据交互与规范化

`go-mini` 自动处理 Go 类型与 VM 规范类型之间的转换：

| Go 类型 | VM 规范类型 | 转换说明 |
| :--- | :--- | :--- |
| `int`, `int64`, `int32` | `Int64` | 自动规约 |
| `float64`, `float32` | `Float64` | 自动规约 |
| `string` | `String` | 值拷贝 |
| `[]byte` | `TypeBytes` | **强制深度拷贝**，确保物理隔离 |
| `struct` | `TypeMap` | 递归映射公开字段（支持 `json` 标签） |
| `pointer` | `TypeHandle` | **句柄映射**，绝对禁止原生地址外泄 |

---

## 🛡️ 安全与沙盒控制

### 1. 指令步数限制 (Step Limit)

防止死循环耗尽 CPU：

```go
prog.SetStepLimit(10000) 
```

### 2. 内存隔离与指针 (`new` / `*p`)

脚本支持标准的指针语法，但其本质是**沙盒句柄**。

```go
p := new(Int64)
*p = 123
fmt.Println(*p) // 输出 123
```
*注：严禁对指针进行加减运算（如 `p++`），这在 VM 内部会触发类型安全错误。*

---

## 🔌 IDE 与 LSP 集成指南

`go-mini` 提供了完善的静态分析 API：

```go
// 获取悬浮提示 (类型签名 + 文档)
hover := prog.GetHoverAt(10, 5)

// 查找所有引用
refs := prog.GetReferencesAt(10, 5)

// 转到定义
defNode := prog.GetDefinitionAt(10, 5)
```

---

## 🛠️ 自定义扩展：FFI 生成器 (`ffigen`)

使用 `ffigen` 将复杂的业务对象注入脚本。它会自动处理对象到句柄的转换：

```go
// ffigen:module order
// ffigen:methods Order
type OrderService interface {
    New(id string) (*Order, error)
    AddItem(o *Order, name string, price float64) error
}
```

运行 `make gen` 后，生成的代码将确保：
1.  脚本中通过 `o.AddItem` 调用的方法被准确路由。
2.  `*Order` 宿主指针永远不会暴露给脚本，脚本仅持有其 ID。

---

## 📝 脚本语法支持清单

### 1. 基础支持
*   **数据类型**：`int`, `int64`, `float64`, `bool`, `string`, `byte`, `[]byte`, `any`。
*   **容器**：数组/切片 (`[]T`)，字典 (`map[string]T`)。
*   **指针**：`new(T)`, `*p` (解引用)。

### 2. 异常处理
*   **Panic**: 支持 `panic("error")`。
*   **Recover**: 支持在 `defer` 中使用 `recover()` 捕获异常。
*   **Try-Catch**: 支持实验性的 `try { ... } catch(e) { ... } finally { ... }` 语法（推荐用于复杂逻辑）。

*(注意：脚本中不支持并发原语如 `go`, `chan`。)*
