# Go-Mini 使用文档

`go-mini` 是一个高性能、绝对内存隔离的 Go 语言子集脚本执行引擎。它被设计用于在宿主 Go 程序中安全地执行不受信的脚本，同时通过 Raw-FFI (Foreign Function Interface) 提供极高的 I/O 性能。

## 🌟 核心特性

*   **绝对安全隔离**：脚本运行在完全独立的虚拟机 (VM) 中，与宿主程序零指针共享。
*   **原生 Go 语法**：脚本完全使用 Go 语言语法编写，无需学习新的领域特定语言 (DSL)。
*   **零反射高性能**：底层通信基于编译期生成的路由代码和二进制序列化，彻底摒弃 `reflect` 包，性能极佳。
*   **资源防泄漏**：内置指令计数器 (`StepLimit`)、上下文感知 (`context.Context`) 和严格的句柄 (Handle) 自动回收机制。
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

### 2. 使用标准库

`go-mini` 默认是“空载”的（没有任何外部能力）。如果你需要让脚本打印日志、读取文件或解析 JSON，你需要注入标准库。

```go
package main

import (
	"context"
	engine "gopkg.d7z.net/go-mini/core"
)

func main() {
	executor := engine.NewMiniExecutor()
	
	// 注入 fmt, os, io, json, time 等标准库的 FFI 绑定
	executor.InjectStandardLibraries()

	code := `
	package main
	import "fmt"
	import "json"

	func main() {
		// 使用 fmt 打印
		fmt.Println("Hello from Go-Mini!")

		// 使用 json 解析 (返回 Result<T> 结构)
		res := json.Unmarshal([]byte(` + "`" + `{"status": "ok"}` + "`" + `))
		if res.err != nil {
			panic(res.err)
		}
		
		// 动态访问 Map 字段
		fmt.Println("Status:", res.val.status)
	}
	`

	prog, _ := executor.NewRuntimeByGoCode(code)
	_ = prog.Execute(context.Background())
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

宿主的重量级资源（如文件）在 VM 中以 `TypeHandle`（一个 ID）的形式存在。即使脚本中途 Panic 或被强制中断，引擎也会在退出时自动通知宿主销毁所有分配的 Handle，绝不泄漏宿主 FD。

---

## 🛠️ 自定义扩展：使用 FFI 生成器 (`ffigen`)

如果你想让脚本调用你自己的 Go 函数（例如操作数据库、请求网络），你需要使用 `ffigen` 工具。

### 步骤 1：定义接口
在你的项目中定义一个普通的 Go 接口。所有可能失败的方法都应该返回 `error`。

```go
// mylib/interface.go
package mylib

type Database interface {
	GetUser(id int64) (string, error)
	SaveData(data []byte) error
}
```

### 步骤 2：生成绑定代码
使用附带的 `ffigen` 命令行工具（或者通过 `go generate`）：

```bash
go run ./cmd/ffigen -pkg mylib -iface Database -out mylib_ffigen.go
```
这会自动生成安全且零反射的 `Router`（宿主端接收器）和 `Proxy`（供 VM 生成 AST 用的代理类型）。

### 步骤 3：实现宿主逻辑并注册
实现你定义的接口，并将其注册到执行器中：

```go
// 宿主端的真实实现
type MyDatabaseImpl struct {}
func (db *MyDatabaseImpl) GetUser(id int64) (string, error) { return "Alice", nil }
func (db *MyDatabaseImpl) SaveData(data []byte) error { return nil }

// 在 main.go 中注册
func main() {
	executor := engine.NewMiniExecutor()
	
	// 假设生成器生成了 RegisterDatabaseFFI 函数
	// mylib.RegisterDatabaseFFI(executor, &MyDatabaseImpl{})
	
	// 现在脚本里就可以 import "mylib" 并调用对应方法了
}
```

---

## 📝 脚本语法支持清单

`go-mini` 支持大部分常用的 Go 过程式语法：

*   **基本数据类型**：`int64`, `float64`, `bool`, `string`, `[]byte`, `any`。
*   **容器类型**：动态数组 (`[]any`)，动态字典 (`map[string]any`)。
*   **控制流**：
    *   `if / else`
    *   `for` (经典三段式)
    *   `for ... range` (支持遍历数组和字典)
    *   `switch / case / default`
    *   `break / continue`
*   **函数与作用域**：支持自定义函数、嵌套作用域、闭包。
*   **错误处理**：通过标准的 `Result` 对象 (`res.val`, `res.err`) 处理，不支持原生 `recover`，但支持简单的 `panic()` 抛出异常中断执行。
*   **资源清理**：支持 `defer` 语句，执行顺序遵循标准的 LIFO（后进先出）。
*   **高级操作**：切片截取 (`buf[1:4]`)，多重/深度赋值 (`m["key"] = 1`, `obj.prop = 2`)。

*(注意：由于隔离原则，脚本中不支持真实的指针操作如 `&` 和 `*`，所有容器类型均按引用隐式传递。)*
