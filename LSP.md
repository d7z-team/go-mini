# Go-Mini LSP 集成指南

`go-mini` 的 LSP / 查询能力建立在源码与语义上下文之上，和运行时执行链各自独立：

- LSP 使用 `core/lspserv`
- Go 源码解析和 tolerant conversion 使用 `core/gofrontend`
- 其他语言前端通过 `core/frontend.Frontend` 进入同一分析链
- 执行使用 `ExecutableProgram` / bytecode

IDE 能力基于 `AnalysisProgram` 持有的源码信息，执行链基于 `ExecutableProgram` / bytecode。

LSP 展示的函数签名和类型文本使用项目统一的 canonical type renderer，例如 `function(Int64, Int64) Int64`、`Map<String, Int64>`、`RecvChan<Int64>`、`Ptr<Int64>` 和 `struct { Name String; }`。Go 风格类型会在分析前规范化；指针表达式按 `Ptr<T>` 语义展示。

编译期调用模板会把自身的源码签名暴露给 LSP；`engine.NewMiniExecutor()` 默认提供 `errors`、`fmt.Errorf`、`strings`、`strconv`、`math`、`sort` 纯库符号。当执行器通过 `executor.UseSurface(ffilib.Surface())` 装配顶层 surface 时，`print` / `println` 模板、完整标准库 FFI 和 `context` 这类 VM 源码库会参与补全、hover 与语义校验。模板 hover 会展示最终渲染后的源码视图，例如 `import "fmt"` 与 `fmt.Println(...)`。

通过 `surface.Library(...)` 注册的纯 VM 源码库会参与语义分析和补全；补全范围限定为库源码自身声明且 ASCII 大写开头的函数、变量、常量、类型、结构体和接口。

源码库进入 LSP 的方式和执行侧一致：通过 `surface.Library(...)` 随 `UseSurface(...)` 装配。

## 1. 推荐后端接入

Go-Mini 提供两类常见接入方式：

- `stdio`：给 VSCode、Neovim 等标准 LSP 客户端使用，直接启动 `examples/cmd/lsp-server`。
- HTTP / RPC：给 Web IDE、Serverless 或自定义后端使用，直接封装 `core/lspserv`。

### stdio LSP

`examples/cmd/lsp-server` 通过标准输入/输出处理 JSON-RPC LSP 消息：

```bash
go run ./examples/cmd/lsp-server
```

编辑器插件只需要把该命令配置为 language server command。服务端支持 initialize、didOpen、didChange、didSave、didClose、workspace/didChangeWatchedFiles、completion、hover、definition、references、signatureHelp、documentSymbol、semanticTokens/full、codeAction、shutdown 和 exit。

stdio LSP 声明 full text sync（`textDocumentSync.change = 1`），客户端应在 didOpen / didChange 中发送完整文本。诊断防抖在 server 侧完成：didChange 只更新会话并延迟发布 diagnostics，didSave 会立即 flush pending diagnostics，didClose 会取消 pending diagnostics 并回退到磁盘文件状态；如果磁盘上同包文件仍存在，诊断不会被错误清空。completion / hover / definition / references / signatureHelp / documentSymbol 查询会基于最新 package snapshot 刷新分析，不等待诊断防抖结束。

### HTTP / RPC API

如果你在 Web IDE 或服务端集成，不必启动 `examples/cmd/lsp-server`。推荐在自己的 HTTP / RPC handler 中复用同一个 `LSPServer`：

```go
executor, err := engine.NewMiniExecutor()
if err != nil {
    return err
}
if err := executor.UseSurface(ffilib.Surface()); err != nil {
    return err
}

lsp := lspserv.NewLSPServer(executor)
```

典型 API 请求流：

1. `UpdateSession(uri, code)`
2. `GetCompletions(uri, line, char)`
3. `GetHover(uri, line, char)`
4. `GetDefinition` / `GetReferences(uri, line, char, includeDeclaration)`
5. `GetSignatureHelp` / `GetDocumentSymbols` / `GetSemanticTokens` / `GetCodeActions`
6. `RefreshWorkspaceFiles(uris)` 处理磁盘文件 create/change/delete
7. `RemoveSession(uri)` 移除打开文件覆盖并回退到磁盘 snapshot

HTTP 形态可以按能力拆成 `/diagnostics`、`/completion`、`/hover`、`/definition`、`/references` 等接口；每个请求先同步当前文件内容，再执行查询即可。`line` / `char` 与 LSP 一样从 0 开始。

## 2. 无状态模式

每次请求都上传当前源码：

```go
diags, _ := lsp.UpdateSession(uri, code)
items := lsp.GetCompletions(uri, line, char)
```

适合：

- HTTP API
- Serverless
- 简单在线编辑器

特点：

- 后端不需要维护复杂会话
- 每次重新解析当前源码
- 更容易水平扩展

## 3. 有状态模式

如果是 WebSocket 或长连接 IDE，可以复用同一个 `LSPServer`：

```go
diags, _ := lsp.UpdateSession(msg.URI, msg.Code)
items := lsp.GetCompletions(msg.URI, msg.Line, msg.Char)
hover := lsp.GetHover(msg.URI, msg.Line, msg.Char)
refs := lsp.GetReferences(msg.URI, msg.Line, msg.Char, true)
```

适合：

- 云 IDE
- 多文件大项目
- 需要持续缓存 session 的场景

## 4. 多文件处理

`UpdateSession` 支持按 URI 维度逐个更新文件。只要这些文件属于同一目录与同一 package，LSP 会按 package snapshot 做合并分析：打开文件使用内存覆盖，未打开的同包 `.mgo` 文件从磁盘读取，workspace watcher 触发 `RefreshWorkspaceFiles` 后会刷新同目录 package。

```go
for filename, code := range req.Files {
    _, _ = lsp.UpdateSession("virtual://project/"+filename, code)
}

items := lsp.GetCompletions("virtual://project/"+req.CurrentFile, req.Line, req.Char)
```

## 5. 诊断

当前诊断能力包括：

- 语法错误
- 语义错误
- 跨文件符号错误
- 模板/编译期检查错误
- lowering / bytecode / prepared validation parity 错误
- UTF-16 LSP range 输出
- 关闭文件后回退到磁盘 snapshot 并发布差异诊断

返回结构遵循 LSP 风格：

```json
[
  {
    "range": {
      "start": { "line": 4, "character": 2 },
      "end": { "line": 4, "character": 14 }
    },
    "severity": 1,
    "source": "go-mini-semantic",
    "code": "semantic",
    "message": "类型不匹配: 无法将 String 赋值给 a (Int64)"
  }
]
```

诊断 source 用于区分阶段：`go-mini-syntax`、`go-mini-semantic`、`go-mini-compile`、`go-mini-lowering`、`go-mini-runtime`。语法 tolerant 节点不会再把同一 parse failure 重复报告为语义错误；上下文推断导致的真实语义错误仍会保留。

## 6. 与执行链的关系

需要明确区分：

- LSP / IDE 查询：基于源码信息
- 运行 / 反汇编 / 持久化：基于 bytecode

channel/select 阻塞、FFI channel endpoint wake、异步 FFI 的 wait-source 分类、`RunController` pause/resume、`VMClock` 冻结和 `VMAllBlockedError` 都属于执行期调度语义。LSP 只通过源码信息与 schema 暴露函数签名、channel 方向、类型和模板信息，不模拟 select 调度、channel readiness、async completion 或运行时 pause/time 状态，也不把这些状态写入分析缓存。

如果你的系统既要编辑又要运行，推荐双链设计：

1. 编辑时走 `lspserv`
2. 运行时走 `CompileGoCode` / `NewRuntimeByArtifact` / `NewRuntimeByBytecodeJSON`

运行入口使用编译产物或 bytecode artifact。

## 7. 当前 IDE 能力

`examples/cmd/lsp-server` 支持标准 stdio JSON-RPC 生命周期，包括 initialize、didOpen、didChange、didSave、didClose、workspace/didChangeWatchedFiles、shutdown 和 exit，并使用 full text sync 与 server 侧 diagnostics debounce。

导航与悬浮覆盖局部变量、全局变量、函数、方法、结构体字段、import 别名、常量、类型别名、结构体和接口声明。references 会按客户端传入的 `includeDeclaration` 决定是否返回声明位置。signature help 使用语义检查后的函数类型与参数信息；document symbols 覆盖函数、结构体、接口、常量、类型与变量；semantic tokens 目前是词法级 token；code action 当前提供缺失 import 的 quick fix。
