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

得益的内容架构，你可以轻松控制脚本的运行节奏。

```go
func main() {
    prog, _ := executor.NewRuntimeByGocode(longRunningCode)
    
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

使用 `ffigen` 将复杂的业务对象注入脚本。它会自动解析 Go 接口或结构体，并生成零反射的高性能桥接代码（包括参数序列化、路由分发和句柄管理）。

### 1. 核心特性：全路径类型安全 (Canonical Paths)

`ffigen` 现在采用 **全路径类型识别** 机制。它会自动推导导入包的完整路径，并以此作为 VM 内部的唯一类型标识。
*   **解决冲突**：即使你的项目中有两个同名的包（例如两个 `other.Page`），引擎也能根据其全路径 ID 精确识别并分发方法调用。
*   **静态校验**：在生成代码时，`ffigen` 会严格校验 `ffigen:methods` 标注的类型与代码中 Receiver 的类型是否完全一致（包括包名前缀），如果不一致将中止生成并报错。

### 2. 多种导出模式

#### A. 接口导出 (Interface-Driven) - 推荐
适合需要解耦和精细控制导出方法的场景。

```go
// ffigen:module browser
// ffigen:methods ops.Page
type PageService interface {
    Locator(p *ops.Page, selectors ...string) (*ops.CdpSelector, error)
}
```

#### B. 结构体导出 (Struct-Direct) - 极简
无需定义接口，直接导出结构体及其指针接收者的所有公开方法。

```go
// ffigen:methods
type Calculator struct { Base Int64 }

// 自动导出 Add 方法
func (c *Calculator) Add(x Int64) Int64 { return c.Base + x }
```
*注：标注在 `struct` 上时，若不指定名称，默认使用该结构体名作为类型前缀。*

### 3. 通过 go:generate 自动生成 (推荐)

在你的接口或结构体文件顶部添加 `//go:generate` 指令，并将其集成到项目的 `make gen` 流程中。

```go
//go:generate go run gopkg.d7z.net/go-mini/cmd/ffigen -pkg orderlib -out order_ffigen.go interface.go
package orderlib

// ffigen:module order
// ffigen:methods other.Order
type OrderService interface {
    New(id String) (*other.Order, Error)
    AddItem(o *other.Order, name String, price Float64) Error
}
```

### 4. 注册到执行引擎

生成代码后，在宿主程序中调用生成的 `RegisterXXX` 函数即可：

```go
func main() {
	executor := engine.NewMiniExecutor()
	registry := executor.HandleRegistry()

	// 注册生成的 FFI 路由
	orderlib.RegisterOrder(executor, &MyOrderImpl{}, registry)
}
```

---

## 💎 Interface (接口) 系统

`go-mini` 提供了一套遵循 Go 语义但经过安全加固的接口系统。它支持鸭子类型（Duck Typing）和运行时动态分发。

### 1. 接口定义

支持命名接口、匿名接口以及 **接口嵌入 (Embedding)**：

```go
type Reader interface {
    Read() String
}

type Writer interface {
    Write(String)
}

// 接口嵌入：自动合并方法集
type ReadWriter interface {
    Reader
    Writer
}
```

### 2. 隐式实现 (鸭子类型)

任何对象只要拥有匹配的方法签名，就自动实现了该接口。支持 `Map`、`Struct` 或是由宿主注入的 `Handle`。

### 3. 类型断言 (Type Assertion)

```go
val, ok := i.(ReadWriter)
if ok {
    val.Write("data")
}
```

### 4. Type Switch

支持带赋值的类型开关以及 **nil 匹配**：

```go
switch x := v.(type) {
case nil:
    return "Object is nil"
case Int64:
    return "Integer: " + String(x)
case ReadWriter:
    return "IO Object"
default:
    return "Unknown"
}
```

### 5. 反向代理 (Reverse Proxy)

这是 `go-mini` 最强大的特性之一：**让宿主侧像调用本地对象一样调用脚本实现的功能**。

**步骤 1：在 Go 侧定义接口并标注**
```go
// ffigen:reverse
type ScriptHandler interface {
    OnEvent(name string, data any) error
}
```

**步骤 2：生成代码并使用**
`ffigen` 会生成 `NewScriptHandler_ReverseProxy`。你只需要从脚本拿到实现的 Map，即可包装成原生的 Go 接口对象：

```go
// Go 侧代码
proxy := NewScriptHandler_ReverseProxy(program, session, scriptMap, bridge)
// 像调用普通 Go 对象一样调用脚本！
err := proxy.OnEvent("login", "user_1")
```

---

## 📝 脚本语法支持清单

### 1. 基础支持
*   **数据类型**：`int`, `int64`, `float64`, `bool`, `string`, `byte`, `[]byte`, `any`。
*   **多返回值**：支持 `func f() (int, string) { return 1, "ok" }` 及其在 FFI 中的自动打包。
*   **容器**：数组/切片 (`[]T`)，字典 (`map[string]T`)。
*   **指针**：`new(T)`, `*p` (解引用)。

### 2. 异常处理
*   **Panic/Recover**: 支持完整的异常抛出与捕获。
*   **Try-Catch**: 实验性的 `try { ... } catch(e) { ... }` 语法。

*(注意：脚本中不支持并发原语如 `go`, `chan`。)*
