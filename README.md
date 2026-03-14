# Go-Mini

Go-Mini 是一个轻量级、零拷贝、语法兼容 Go 的脚本执行引擎。它允许你在 Go 应用程序中嵌入并安全地执行 Go 风格的脚本代码，并提供了完善的类型校验和原生互操作能力。

## 核心特性

- **Go 语法兼容**: 支持结构体（Struct）、函数（Function）、指针（Pointer）、循环（For）、条件判断（If/Else）、多分支 switch、短路求值等核心语法。
- **强类型校验**: 内置完善的 AST 校验器，支持跨包类型推导与方法签名验证。
- **Zero-Copy Proxy 机制**: 原生函数交互无需深拷贝，通过代理接口 (`ast.MiniArray`, `ast.MiniMap`, `ast.MiniStruct`) 直接暴露底层内存，支持原生侧对脚本状态的直接修改（副作用同步）。
- **原生互互操作**: 能够轻松将 Go 的结构体和函数注册到脚本环境中，支持变长参数自动打包与解包。
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
	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/runtimes"
)

func main() {
	// 创建执行器并初始化标准库
	executor := engine.NewMiniExecutor()
	runtimes.InitAll(executor)

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

## 完整使用文档

对于详细的 Proxy 架构使用方法、结构体挂载、数组与字典的数据操作交互等，请参考详细文档：
[👉 Go-Mini 使用说明文档 (DOC.md)](./DOC.md)

## 项目结构

- `core/ast`: 抽象语法树定义及强类型校验逻辑。
- `core/runtime`: 脚本运行时与执行引擎（含 Zero-Copy 适配器）。
- `runtimes/`: 预定义的标准库实现（Fmt, FS, IO, OS, Strings, Time）。
- `core/e2e/`: 端到端测试用例，覆盖各种复杂语法与 Proxy 边界场景。

## 开发与测试

运行所有测试：

```bash
go test ./...
```

## 许可证

MIT License
