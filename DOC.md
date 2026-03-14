# Go-Mini 使用说明文档 (DOC.md)

Go-Mini 是一个轻量级、零拷贝、强类型的 Go 脚本执行引擎。它允许你在 Go 应用程序中嵌入并安全地执行 Go 风格的脚本代码，并支持 Native 代码与脚本之间高效的数据互操作。

---

## 1. 核心概念与架构

*   **MiniExecutor**: 引擎实例的核心，负责环境隔离、原生函数注册以及全局类型的管理。
*   **Proxy 机制 (Zero-Copy)**: 针对容器类型（Array/Map/Struct），引擎不再使用深拷贝传递数据，而是通过 Proxy 接口（`ast.MiniArray`, `ast.MiniMap`, `ast.MiniStruct`）直接暴露底层内存给 Native，实现性能最大化和状态的双向同步（副作用）。

---

## 2. 基础快速开始

```go
package main

import (
	"context"
	"fmt"
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/runtimes"
)

func main() {
	// 1. 初始化引擎
	executor := engine.NewMiniExecutor()

	// 2. 挂载内置标准库 (fmt, strings, time, 等)
	runtimes.InitAll(executor)

	// 3. 注入一个自定义的原生函数
	executor.MustAddFunc("Greet", func(name string) {
		fmt.Printf("Hello, %s!\n", name)
	})

	// 4. 编写脚本
	code := `
		package main
		func main() {
			Greet("World")
		}
	`

	// 5. 解析并执行
	rt, err := executor.NewRuntimeByGoCode(code)
	if err != nil {
		panic(err)
	}
	
	err = rt.Execute(context.Background())
	if err != nil {
		panic(err)
	}
}
```

---

## 3. 高级数据交互 (Proxy 与副作用)

为了保证性能，VM 的内部状态与 Go Native 之间采用**代理模式**通信。

### 3.1 传递与修改数组 (MiniArray)

当你的原生函数需要接收并修改脚本中的数组时，必须使用 `ast.MiniArray` 作为参数类型。

**Native 侧定义:**
```go
import "gopkg.d7z.net/go-mini/core/ast"

executor.MustAddFunc("ModifyArray", func(arr ast.MiniArray) {
    // 读取元素
    val, _ := arr.Get(0)
    fmt.Println(val)

    // 修改元素 (副作用)
    newVal := ast.NewMiniString("Modified")
    arr.Set(0, &newVal)
})
```

**脚本侧执行:**
```go
code := `
    func main() {
        list := []string{"Original"}
        ModifyArray(list)
        // 此时 list[0] 已经变成了 "Modified"
    }
`
```

### 3.2 传递与修改 Map (MiniMap)

同样地，字典数据需要使用 `ast.MiniMap`。

**Native 侧定义:**
```go
executor.MustAddFunc("ModifyMap", func(m ast.MiniMap) {
    k := ast.NewMiniString("status")
    v := ast.NewMiniString("ready")
    m.Set(&k, &v) // 设置键值对
})
```

### 3.3 注册并操作结构体 (MiniStruct)

你可以将 Go 的结构体注册进环境，并在 Native 代码中操作脚本实例化出来的结构体对象。

**Native 侧注册:**
```go
// 定义原生结构体
type User struct {
    Name *ast.MiniString
    Age  *ast.MiniInt64
}

// 注册供脚本使用
executor.AddNativeStruct((*User)(nil))

// 定义操作函数
executor.MustAddFunc("GrowUp", func(s ast.MiniStruct) error {
    val, _ := s.GetField("Age")
    // 使用 UnwrapProxy 解析出底层的 MiniObj
    val = runtime.UnwrapProxy(val).(ast.MiniObj)
    
    if age, ok := val.(*ast.MiniInt64); ok {
        newAge := ast.NewMiniInt64(age.GoValue().(int64) + 1)
        return s.SetField("Age", &newAge)
    }
    return nil
})
```

**脚本侧执行:**
```go
code := `
    func main() {
        u := User{Name: "Alice", Age: 18}
        GrowUp(u)
        // 此时 u.Age 变为 19
    }
`
```

---

## 4. 脚本语法支持特性

Go-Mini 支持绝大部分 Go 的语法范式：

*   **基础类型**: 
    *   整型：`int`, `int8` ~ `int64`, `uint`, `uint8` ~ `uint64`
    *   浮点型：`float32`, `float64`
    *   复数：`complex64`, `complex128`
    *   布尔与字符串：`bool`, `string`
*   **复合类型**: 
    *   数组/切片：`[]T`
    *   字典：`map[K]V`
*   **控制流**: 
    *   `if / else if / else`，支持短路求值 (`&&`, `||`)。
    *   `for` 循环（包括 `break` 和 `continue`），支持 `for range` 迭代数组和字典。
    *   `switch / case / default`。
    *   `defer` 延迟执行。
*   **操作符**: 
    *   数学：`+`, `-`, `*`, `/`, `%`
    *   比较：`==`, `!=`, `<`, `>`, `<=`, `>=`
    *   一元：`!`, `-`, `+`, `^` (按位取反)
    *   复合赋值：`+=`, `-=`, `*=`, `/=`, `%=`
*   **函数特性**: 
    *   多返回值。
    *   命名返回值。
    *   变长参数（在 Native 会被自动打包为 `MiniArray`）。
    *   指针操作（`&` 取地址，`*` 解引用）。

---

## 5. 内置标准库参考

通过 `runtimes.InitAll(executor)`，将默认装载以下包：

*   **`fmt`**: 提供 `fmt.Sprintf` 等。
*   **`strings`**: 提供 `Contains`, `ToUpper`, `Split`, `Join`, `Replace` 等。
*   **`os`**: 提供文件读写等基础 OS 映射。
*   **`time`**: 提供 `time.Now()`, `time.Unix()` 以及时间对象的 `Format` 等。
*   **`fs`**: 提供基于 afero 的内存/系统抽象文件系统操作。

## 6. 注意事项

1.  **强类型校验**: 引擎在执行前会进行完整的 AST 类型推导与校验。如果传入参数的类型与函数签名不匹配，将在解析阶段报错，而不会留到运行时崩溃。
2.  **Native 函数签名**: 为了利用 Zero-Copy 机制，原生 Go 函数不可使用 `[]string` 或 `map[string]int` 作为参数，必须使用 `ast.MiniArray` 和 `ast.MiniMap` 等代理接口，并在内部进行类型转换或断言。
3.  **并发安全**: 同一个 `MiniExecutor` 可以并发创建多个不同的 `MiniProgram` 实例；单个 `MiniProgram.Execute(ctx)` 在独立的协程中是安全的，但如果通过闭包或指针在不同脚本实例间共享了内存对象，则需要开发者自行保证同步互斥。
