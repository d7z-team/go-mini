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
	import "os"
	import "fmt"

	func main() {
		// 使用 os.Create 创建文件，返回 Result<Ptr<File>>
		res := os.Create("hello.txt")
		if res.err != nil {
			panic(res.err)
		}
		f := res.val

		// 面向对象风格的方法调用：写入数据
		f.Write([]byte("Hello from Go-Mini OOP!"))
		
		// 关闭文件
		f.Close()

		// 读取验证
		dataRes := os.ReadFile("hello.txt")
		fmt.Println("Content:", string(dataRes.val))
		
		// 清理
		os.Remove("hello.txt")
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

### 步骤 2：生成绑定代码
使用 `ffigen` 工具：

```bash
go run ./cmd/ffigen -pkg mylib -out mylib_ffigen.go interface.go
```

### 步骤 3：实现并注册
```go
// 宿主逻辑
type MyDatabaseImpl struct {}
func (db *MyDatabaseImpl) GetUser(id int64) (string, error) { return "Alice", nil }
func (db *MyDatabaseImpl) SaveData(data []byte) error { return nil }

func main() {
	executor := engine.NewMiniExecutor()

	// 使用生成的精简注册函数
	mylib.RegisterDatabase(executor, &MyDatabaseImpl{}, executor.HandleRegistry())
}
```
现在脚本里就可以 `import "db"` 并直接调用 `db.GetUser(1)` 了。


---

## 📝 脚本语法支持清单

`go-mini` 支持绝大部分常用的 Go 过程式和面向对象语法：

### 1. 基础与数据类型
*   **基本数据类型**：`int` (等同于 `int64`), `int64`, `float64`, `bool`, `string`, `byte`, `[]byte`, `any`。
*   **容器类型**：动态数组/切片 (`[]T`)，动态字典 (`map[string]T`)。
*   **结构体**：支持自定义 `type struct { ... }` 及其复合字面量初始化。
*   **类型转换**：内置了 `int()`, `int64()`, `float64()`, `string()`, `[]byte()` 用于基础类型间的显式转换。

### 2. 函数与面向对象 (OOP)
*   **函数定义**：支持标准的 `func` 定义，包括多参数和多返回值。
*   **变长参数 (Variadic)**：支持定义和调用接收不定数量参数的函数，例如 `func sum(args ...int) int`。
*   **方法接收者 (Method Receivers)**：完全支持为结构体定义方法，例如 `func (p Person) GetAge() int`，支持通过 `p.GetAge()` 调用（引擎底层采用无反射的轻量级路由，性能极高）。

### 3. 控制流与变量操作
*   **变量声明**：支持标准 `var` 声明和短变量声明 `:=`（支持类型自动推导）。
*   **多变量解构赋值**：支持通过 `a, b := f()` 来解构函数的多返回值，也支持复杂的混合赋值分发（如 `arr[1], p.X = 10, 20`）。
*   **自增与自减**：`++` 和 `--`（不仅支持普通变量，还支持结构体成员 `p.Age++` 和数组/Map 索引）。
*   **条件与循环**：
    *   `if / else`
    *   `for` (经典三段式)
    *   `for ... range` (支持遍历数组和字典)
    *   `switch / case / default`
    *   `break / continue`

### 4. 高级操作与错误处理
*   **引用语义**：**重要**。为了性能优化，脚本内定义的结构体和数组采用**引用语义**。赋值（`a := b`）或将对象作为方法接收者时，修改其中一个变量会影响另一个。
*   **Context 数据传递**：执行脚本时通过 `Execute(ctx)` 传入的 Context 会自动透传给所有带 `context.Context` 参数的 FFI 方法。这允许宿主程序动态向脚本调用链注入数据（通过 `context.WithValue`）。
*   **内存预分配**：支持通过 `make([]int, len, cap)` 或 `make(map[string]any)` 预分配内存。
*   **动态集合操作**：内建支持 `append(slice, ...items)` 用于向数组追加元素，支持 `delete(map, key)` 用于从字典中移除键值对。
*   **模块与包加载系统**：支持 `import "pkg"`。执行器内置了真正的动态模块加载器，允许脚本引用其他脚本文件，共享导出（首字母大写）的常量、变量、结构体和函数。
*   **错误处理**：通过标准的 `Result` 对象 (`res.val`, `res.err`) 与多变量解构完美结合处理。引擎秉持 Fail-Fast 哲学，不支持原生 `recover`，但支持通过 `panic()` 抛出致命异常。
*   **资源清理**：支持 `defer` 语句，执行顺序遵循标准的 LIFO（后进先出）。

*(注意：由于绝对隔离原则，脚本中不支持并发原语如 `go`, `chan`, `select`，严禁使用 `&` 和 `*` 进行原生指针算术计算。)*
