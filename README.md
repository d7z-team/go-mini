# Go-Mini

Go-Mini 是一个轻量级的、语法兼容 Go 的脚本执行引擎。它允许你在 Go 应用程序中动态执行 Go 风格的脚本，并提供了完善的类型校验和原生互操作能力。

## 核心特性

- **Go 语法兼容**: 支持结构体（Struct）、函数（Function）、指针（Pointer）、循环（For）、条件判断（If/Else）等核心语法。
- **强类型校验**: 内置完善的 AST 校验器，支持跨包类型推导与方法签名验证。
- **泛型集合**: 提供单态化的 `Array<T>` 和 `Map<K, V>`，内置 `get`, `length`, `push`, `keys` 等常用方法。
- **原生互互操作**: 能够轻松将 Go 的结构体和函数注册到脚本环境中。
- **内置标准库**: 预集成了 `fmt`, `os`, `time`, `strings`, `fs`, `io` 等常用模块。

## 快速开始

### 安装

```bash
go get gopkg.d7z.net/go-mini
```

### 基础用法

```go
package main

import (
	"context"
	"fmt"
	"gopkg.d7z.net/go-mini"
)

func main() {
	// 创建执行器并初始化标准库
	executor := go_mini.NewMiniScriptExecutor()

	// 编写脚本
	code := `
		import "fmt"
		import "strings"

		func main() {
			s := "Hello, Go-Mini!"
			upper := strings.ToUpper(s)
			fmt.Sprintf("Result: %s", upper)
		}
	`

	// 编译并执行
	rt, _ := executor.NewRuntimeByGoCode(code)
	_ = rt.Execute(context.Background())
}
```

## 项目结构

- `core/ast`: 抽象语法树定义及校验逻辑。
- `core/runtime`: 脚本运行时与执行引擎。
- `runtimes/`: 预定义的标准库实现（Fmt, FS, IO, OS, Strings, Time）。
- `core/e2e/`: 端到端测试用例，覆盖各种复杂语法场景。

## 开发与测试

运行所有测试：

```bash
go test ./...
```

## 许可证

MIT License
