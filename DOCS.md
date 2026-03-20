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
		// 返回值会被特殊变量 __return__ 捕获（如果需要）
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

如果你只需要计算单个 Go 表达式（例如规则引擎场景），可以使用更轻量级的 `Eval` 方法。它不需要 `package` 声明和 `main` 函数，且支持直接传入 Go 原生类型作为环境变量。

```go
func main() {
    executor := engine.NewMiniExecutor()
    
    // 直接传入 Go 原生类型，引擎会自动进行类型转换和内存隔离（[]byte 会被深度拷贝）
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
    
    // 使用 Interface() 轻松转换回 Go 原生接口类型
    fmt.Println("Result:", result.Interface()) // 输出: 51
}
```

### 3. 数据交互与规范化 (Normalization)

`go-mini` 在宿主边界提供了极高的易用性，它会自动处理 Go 类型与 VM 规范类型之间的转换：

| Go 类型 | VM 规范类型 | 转换说明 |
| :--- | :--- | :--- |
| `int`, `int64`, `uint32` | `Int64` | 自动规约，防止溢出 |
| `float64`, `float32` | `Float64` | 自动规约 |
| `string` | `String` | 值拷贝 |
| `[]byte` | `TypeBytes` | **强制深度拷贝**，确保物理隔离 |
| `struct` | `TypeMap` | 递归映射公开字段，优先遵循 `json` 标签 |
| `map`, `slice` | `TypeMap`, `TypeArray` | 递归重建容器，实现内存解耦 |

通过 `Var.Interface()` 方法，你可以将执行结果递归地转换回 Go 原生的 `map[string]any` 或 `[]any`。

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

    // 2. 执行代码片段，它可以识别 Point 类型和 Add 函数
    code := `
        p := utils.Point{X: 1, Y: 2}
        res := utils.Add(p.X, 100)
        return res
    `
    // engine.Execute 会自动注入符号并执行 Check 语义校验
    _ = executor.Execute(context.Background(), code, nil)
}
```

---

## 🛡️ 安全与沙盒控制

`go-mini` 提供了强大的控制手段来防止恶意或有缺陷的脚本耗尽宿主资源。

### 1. 指令步数限制 (Step Limit)

防止死循环（如 `for {}`）耗尽 CPU：

```go
prog, _ := executor.NewRuntimeByGoCode(code)

// 限制脚本最多执行 10000 条基本指令
prog.SetStepLimit(10000)

err := prog.Execute(context.Background())
// 如果超时，err 将返回 "instruction limit exceeded"
```

### 2. Context 取消与超时

防止长时间阻塞的 FFI 调用（结合 `context.WithTimeout`）：

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

// 如果脚本执行超过 2 秒，将会被安全中断
err := prog.Execute(ctx)
```

### 3. 资源自动回收 (Defer & Handle)

宿主的重量级资源（如文件）在 VM 中以 `TypeHandle`（一个 ID）的形式存在。
**三层回收保障：**
1. **显式 Defer**: 脚本内部的 `defer f.Close()` 遵循 Go 标准语义。
2. **会话兜底**: `Execute/Eval` 结束时，Session 记录的所有 `ActiveHandles` 会被强制回收。
3. **GC 最终防线**: 利用 Go 1.24 `runtime.AddCleanup`，当变量被销毁且 GC 时自动释放物理资源。

### 4. 递归分配防御

为了防止脚本通过构造循环引用的结构体 AST 来耗尽宿主内存，`initializeType` (即 `new` 操作) 拥有 **10 层最大深度限制**。超过限额的嵌套将返回 `nil`。

---

## 🐛 源码级调试器 (Debugger)

`go-mini` 内置了一个零开销、基于阻塞 Channel 实现的源码级调试器。它支持**行断点**、**单步执行 (Step Into)**、**随时暂停/恢复**以及**变量快照导出**，完美支持循环体内的调试以及代码片段 (Snippet) 的相对行号调试。

### 使用方法

通过将 `debugger.Session` 注入到执行的 `Context` 中即可开启调试模式：

```go
import "gopkg.d7z.net/go-mini/core/debugger"

func main() {
    // ... 初始化 prog ...

    dbg := debugger.NewSession()
    dbg.AddBreakpoint(5) // 在 b := 20 处暂停

    ctx := debugger.WithDebugger(context.Background(), dbg)

    // 启动执行
    go prog.Execute(ctx)

    // 随时暂停执行
    // 宿主调用 RequestPause 后，VM 将在执行到下一条语句时自动触发事件并进入阻塞状态
    dbg.RequestPause() 

    // 5. 监听调试事件并控制执行
    for event := range dbg.EventChan {
        fmt.Printf("程序暂停在: %d 行\n", event.Loc.L)

        // 打印当前作用域的所有变量快照
        for name, val := range event.Variables {
            fmt.Printf("变量 %s = %v\n", name, val)
        }

        // 发送控制指令解除阻塞 (即恢复执行)
        // 可以发送 debugger.CmdContinue 或 debugger.CmdStepInto
        dbg.CommandChan <- debugger.CmdContinue 
    }
    }
    ```

> **注意**：调试器仅对包含**语句 (Statement)** 的执行模式 (`NewRuntimeByGoCode` 和 `Execute`) 有效。由于纯表达式 (`Eval` 模式，如 `1+2`) 底层瞬间求值不包含语句骨干，因此不会被调试器拦截（除非表达式内部调用了用户自定义的脚本函数）。

---

## 🛠️ 自定义扩展：使用 FFI 生成器 (`ffigen`)

如果你想让脚本调用你自己的 Go 函数（例如操作数据库、请求网络），你需要使用 `ffigen` 工具。

### 步骤 1：定义接口
在你的 Go 代码中定义一个接口，并使用注解来指导生成器。

```go
// mylib/interface.go
package mylib

// ffigen:module db
type Database interface {
	GetUser(id int64) (string, error)
	SaveData(data []byte) error
}
```
*   `// ffigen:module <name>`: 指定脚本中 import 的包名。
*   `// ffigen:methods <TypeName>`: (可选) 用于定义面向对象的方法句柄前缀。

### 步骤 2：设计数据交换语义
`ffigen` 根据你的接口参数和返回类型自动决定数据交换方式：
*   **值语义 (T)**：如果方法返回或接收结构体值，`ffigen` 会触发全量二进制序列化。注意：脚本内定义的结构体对应这些对象时采用引用语义。
*   **句柄语义 (*T)**：如果方法返回或接收指针，`ffigen` 会将其自动注册为 `TypeHandle` (uint32 ID)。VM 无法直接访问其内部字段，只能通过 FFI 方法操作。

---

## 📝 脚本语法支持清单

`go-mini` 支持绝大部分常用的 Go 过程式和面向对象语法：

### 1. 基础与数据类型
*   **基本数据类型**：`int`, `int64`, `float64`, `bool`, `string`, `byte`, `[]byte`, `any`。
*   **容器类型**：动态数组/切片 (`[]T`)，动态字典 (`map[string]T`)。
*   **内建分配器**：
    - `make(T, len, cap)`：支持数组和 Map。
    - `new(T)`：返回深度初始化的零值变量。对于结构体返回引用语义对象（Map 模拟），对于基础类型返回零值（值语义）。
*   **类型转换**：内置了 `Int64()`, `Float64()`, `String()`, `TypeBytes()`。

### 2. 引用语义 (重要)
为了极致的高性能与物理隔离，脚本内的复合类型（Struct, Array, Map）采用**隐式引用语义**。
```go
p1 := Point{X: 1}
p2 := p1 // p2 与 p1 共享底层数据
p2.X = 100
// 此时 p1.X 也是 100
```
这在行为上完美契合了 Go 语言通过指针操作结构体的习惯，但在底层彻底消除了指针算术风险。

### 3. 函数与控制流
*   **函数定义**：支持标准的 `func` 定义、多参数、多返回值、变长参数 (`...any`)。
*   **匿名函数与闭包**：支持 `func() { ... }` 捕获外部环境变量。
*   **错误处理**：支持 `panic()` 抛出异常，支持原生 `defer func() { recover() }()` 截获异常。
*   **循环与分支**：完全支持 `if/else`, `for`, `for...range`, `switch/case`, `break/continue`。

*(注意：脚本中不支持并发原语如 `go`, `chan`, `select`，严禁使用原生指针算术计算。)*
