# Mini-Go

Mini-Go 是一个用 Go 编写的嵌入式脚本引擎，使用接近 Go 的语法。你可以在 Go 应用中编译并调用脚本，
也可以用 `mini-go` 命令直接运行 `.mgo` 文件，编写小工具。

**[快速开始](#快速开始) · [嵌入 Go](#在-go-中使用) · [使用指南](./USAGE.md) · [标准库](./docs/reference/README.md) · [RPC](./RPC.md)**

## 主要功能

- **Go-like 语言**：泛型、闭包、channel/select、defer/panic/recover、反射和嵌入资源。
- **应用内执行**：复用编译结果，创建独立脚本实例，设置取消、执行步数和内存限制。
- **宿主扩展**：通过 FFI 接入 Go 功能，通过生成的 RPC 客户端调用本地或远程服务。
- **常驻脚本**：热更新代码，附加调试符号，使用断点和单步调试。
- **开发工具**：编译缓存、本地源码装配、格式化、文档生成，以及 LSP/DAP 编辑器集成。

随引擎提供字符串、容器、编码、模板等精选标准库。Mini-Go 面向脚本场景，语言与 API 的支持范围见
[使用指南](./USAGE.md)和[标准库参考](./docs/reference/README.md)。

## 安装

需要 Go 1.26 或更新版本。

```bash
go install github.com/d7z-team/mini-go/cmd/mini-go@latest
```

请将 Go 的可执行文件安装目录（`GOBIN`，未设置时为 `GOPATH/bin`）加入 `PATH`。
作为 Go 依赖使用时，见下方[嵌入示例](#在-go-中使用)。

## 快速开始

创建 `hello.mgo`：

```go
package main

func main() {
	println("Hello, Mini-Go!")
}
```

```bash
mini-go check hello.mgo
mini-go run hello.mgo
```

输出 `Hello, Mini-Go!`。多文件可写为 `mini-go run main.mgo helper.mgo`。
Mini-Go 源码统一使用 `.mgo`，测试文件使用 `_test.mgo`；Go 宿主代码使用 `.go`。
目录模式用 `-module` 声明导入前缀，额外本地源码通过 `-source` 提供，详见[命令行使用](./USAGE.md#cli)。

## 在 Go 中使用

在宿主项目中添加依赖：

```bash
go get github.com/d7z-team/mini-go
```

嵌入流程为：提供源码 → 创建 Engine → 编译 Program → 创建 Instance → 调用入口。
Program 可复用，实例状态相互独立。可直接运行的代码见[完整嵌入示例](USAGE.md#完整嵌入示例)，
源码装配、宿主能力、执行控制和热更新见[使用指南](USAGE.md)。

## 文档

接入与使用：

| 文档 | 内容 |
| --- | --- |
| [使用指南](./USAGE.md) | 嵌入 API、CLI、源码装配、执行控制与调试 |
| [RPC 使用指南](./RPC.md) | 声明接口、实现 Go handler、脚本调用和资源关闭 |
| [标准库参考](./docs/reference/README.md) | 从源码生成的包与 API 文档 |
| [VS Code 扩展](./vscode-ext/README.md) | 语法高亮与语言服务配置 |
| [Rust 运行时](./playground/runtime-rust/README.md) · [使用指南](./playground/runtime-rust/USAGE.md) | 原生调用、取消、异步接入、调试与热更新 |
| [浏览器与 Node.js](./playground/runtime-rust/runtime-wasm/README.md) | TypeScript SDK、Worker、WASM 与语言工具 |

## 参与开发

开始修改前阅读[架构说明](./ARCHITECTURE.md)与[开发指南](./DEVELOPMENT.md)，
测试数据的归属见[共享测试数据](./testdata/README.md)。
在仓库根运行 `make help` 查看构建、生成与各后端验证入口。
提交问题时请附上最小 `.mgo` 示例、执行命令、预期行为和实际结果。

## 许可证

项目使用 [MIT License](./LICENSE)。从 Go 标准库移植的源码保留其版权声明，适用 [Go 许可证](./stdlib/LICENSE_GO)。
