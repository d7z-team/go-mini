# Go-Mini 使用文档

`go-mini` 是一个高性能、绝对内存隔离的 Go 语言子集脚本执行引擎。它被设计用于在宿主 Go 程序中安全地执行不受信的脚本，同时通过 Raw-FFI (Foreign Function Interface) 提供极高的 I/O 性能。

> **⚠️ 运行要求**：由于采用了新一代资源回收机制 `runtime.AddCleanup`，宿主环境需使用 **Go 1.24** 或更高版本。

## 🌟 核心特性

*   **绝对安全隔离**：脚本运行在完全独立的虚拟机 (VM) 中，与宿主程序零指针共享。
*   **物理级内存防护**：环境变量中的 `[]byte` 会被自动执行**深度拷贝**，彻底杜绝通过共享切片导致的内存逃逸。
*   **自动化数据映射**：宿主可以直接注入 Go `struct`，引擎会自动利用反射将其规范化为 VM 可识别的 Map（支持 `json` 标签）。
*   **原生 Go 语法**：脚本完全使用 Go 语言语法编写，无需学习新的领域特定语言 (DSL)。
*   **零反射高性能**：底层通信基于编译期生成的路由代码和二进制序列化，彻底摒弃 `reflect` 包，性能极佳。
*   **高并发与线程安全**：执行器采用**无状态蓝图 (Stateless Blueprint)** 设计。同一个编译好的脚本可以在成千上万个宿主 Goroutine 中并发执行，互不干扰。`MiniExecutor` 内部持有读写锁保护符号表。
*   **物理分发能力**：脚本蓝图（AST）完全可序列化为 JSON。你可以在中心节点编译脚本，通过网络分发到任意边缘节点，实现零成本恢复执行。
*   **资源防泄漏**：内置指令计数器 (`StepLimit`)、上下文感知 (`context.Context`) 和严格的句柄 (Handle) 自动回收机制。即便脚本 Panic，打开的文件描述符也会在会话结束时被强制销毁。
*   **易扩展的 FFI**：通过附带的 `ffigen` 工具，极速生成自定义的宿主-脚本绑定。

---

## 🚀 快速开始

### 1. 基础执行

要在你的 Go 程序中执行一段 `go-mini` 脚本，你需要创建一个 `MiniExecutor`，将脚本编译为 `MiniProgram`，然后执行。

```go
package main

import (
	"context"
	"fmt"
	engine "gopkg.d7z.net/go-mini/core"
)

func main() {
	// 1. 创建执行器
	executor := engine.NewMiniExecutor()

	// 2. 编写脚本代码
	code := `
	package main
	func main() {
		x := 10
		y := 20
	}
	`

	// 3. 编译脚本
	prog, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		panic(err)
	}

	// 4. 执行脚本
	err = prog.Execute(context.Background())
	if err != nil {
		panic(err)
	}
	fmt.Println("脚本执行成功！")
}
```

### 2. 表达式执行 (Eval)

如果你只需要计算单个 Go 表达式（例如规则引擎场景），可以使用更轻量级的 `Eval` 方法。它支持直接传入 Go 原生类型作为环境变量。

```go
func main() {
    executor := engine.NewMiniExecutor()
    
    env := map[string]interface{}{
        "a": 10,
        "b": 20,
        "data": []byte{1, 2, 3},
    }
    
    // 执行表达式
    result, err := executor.Eval(context.Background(), "a + b * 2 + int64(data[0])", env)
    if err != nil {
        panic(err)
    }
    
    fmt.Println("Result:", result.Interface()) // 输出: 51
}
```

### 3. 数据交互与规范化 (Normalization)

`go-mini` 自动处理 Go 类型与 VM 规范类型之间的转换：

| Go 类型 | VM 规范类型 | 转换说明 |
| :--- | :--- | :--- |
| `int`, `int64` | `Int64` | 自动规约 |
| `float64` | `Float64` | 自动规约 |
| `string` | `String` | 值拷贝 |
| `[]byte` | `TypeBytes` | **强制深度拷贝**，确保物理隔离 |
| `struct` | `TypeMap` | 递归映射公开字段（支持 `json` 标签） |
| `map`, `slice` | `TypeMap`, `TypeArray` | 递归重建容器，实现内存解耦 |

### 4. 代码片段执行与符号注入 (Snippet Mode)

`go-mini` 支持执行不完整的代码片段，并能自动“偷取”已注册模块的符号。

```go
func main() {
    executor := engine.NewMiniExecutor()

    // 1. 注册一个工具库
    libProg, _ := executor.NewRuntimeByGoCode(`
        package utils
        type Point struct { X, Y int }
        func Add(a, b int) int { return a + b }
    `)
    executor.RegisterModule("utils", libProg)

    // 2. 执行代码片段
    code := `
        p := utils.Point{X: 1, Y: 2}
        return utils.Add(p.X, 100)
    `
    _ = executor.Execute(context.Background(), code, nil)
}
```

---

## 🛡️ 安全与沙盒控制

### 1. 指令步数限制 (Step Limit)

防止死循环耗尽 CPU：

```go
prog, _ := executor.NewRuntimeByGoCode(code)
prog.SetStepLimit(10000) 
err := prog.Execute(context.Background())
```

### 2. Context 取消与超时

防止长时间阻塞的 FFI 调用：

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
err := prog.Execute(ctx)
```

---

## 🔌 IDE 与 LSP 集成指南

`go-mini` 提供了完善的静态分析 API，支撑 IDE 插件开发。

### 1. 全量语义诊断 (Diagnostics)

在编译脚本时，系统将返回 `*ast.MiniAstError`。它包含了一组结构化的日志，支持 4 坐标精准定位。

```go
prog, err := executor.NewRuntimeByGoCode(invalidCode)
if err != nil {
    if astErr, ok := err.(*ast.MiniAstError); ok {
        for _, log := range astErr.Logs {
            fmt.Printf("错误: %s, 位置: %d:%d\n", log.Message, log.Node.GetBase().Loc.L, log.Node.GetBase().Loc.C)
        }
    }
}
```

### 2. 符号跳转与悬浮 (Navigation & Hover)

通过 `MiniProgram` 的高层 API 实现秒级交互：

```go
// 转到定义
defNode := prog.GetDefinitionAt(10, 5)

// 获取悬浮提示 (类型签名 + 文档)
hover := prog.GetHoverAt(10, 5)

// 查找所有引用
refs := prog.GetReferencesAt(10, 5)
```

### 3. 性能管理：释放缓存

LSP 相关查询依赖 `ParentMap` 懒加载缓存。若在 IDE 模式下频繁查询后希望释放内存，可调用：

```go
prog.ReleaseLSPCache() 
```

---

## 🐛 源码级调试器 (Debugger)

`go-mini` 内置了一个源码级调试器。它支持**行断点**、**单步执行**、**随时暂停**以及**变量快照导出**。

```go
dbg := debugger.NewSession()
dbg.AddBreakpoint(5)

ctx := debugger.WithDebugger(context.Background(), dbg)
go prog.Execute(ctx)

for event := range dbg.EventChan {
    fmt.Printf("暂停在 %d 行, 变量快照: %v\n", event.Loc.L, event.Variables)
    dbg.CommandChan <- debugger.CmdContinue
}
```

---

## 🛠️ 自定义扩展：FFI 生成器 (`ffigen`)

使用 `ffigen` 极速生成宿主绑定：

```go
// ffigen:module db
type Database interface {
	GetUser(id int64) (string, error)
	SaveData(data []byte) error
}
```

---

## 📝 脚本语法支持清单

### 1. 基础支持
*   **基本数据类型**：`int`, `int64`, `float64`, `bool`, `string`, `byte`, `[]byte`, `any`。
*   **容器类型**：动态数组/切片 (`[]T`)，动态字典 (`map[string]T`)。
*   **内建分配器**：`make(T, len, cap)`, `new(T)`。

### 2. 引用语义 (重要)
脚本内的复合类型（Struct, Array, Map）采用**隐式引用语义**。赋值操作不会触发深度拷贝，行为类似于 Go 指针。

### 3. 函数与控制流
*   **函数定义**：支持标准的 `func` 定义、多参数、多返回值、变长参数 (`...any`)、闭包。
*   **错误处理**：支持 `panic()` 和 `defer/recover()`。
*   **循环与分支**：完全支持 `if/else`, `for`, `for...range`, `switch/case`, `break/continue`。

*(注意：脚本中不支持并发原语如 `go`, `chan`，严禁使用原生指针算术计算。)*
