# Go-Mini LSP 集成指南

`go-mini` 的 LSP / 查询能力建立在 AST 蓝图与语义上下文之上，和运行时执行链各自独立：

- LSP 使用 `core/ast` 与 `core/lspserv`
- 执行使用 compiled artifact / prepared program / bytecode

IDE 能力基于源码和 AST，执行链基于 compiled artifact / prepared program / bytecode。

## 1. 推荐后端接入

如果你在 Web IDE 或服务端集成，不必启动 `cmd/lsp-server`。推荐直接封装 `core/lspserv`：

```go
executor := engine.NewMiniExecutor()
executor.InjectStandardLibraries()

lsp := lspserv.NewLSPServer(executor)
```

典型请求流：

1. `UpdateSession(uri, code)`
2. `GetCompletions(uri, line, char)`
3. `GetHover(uri, line, char)`
4. `GetDefinition` / `GetReferences`

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
```

适合：

- 云 IDE
- 多文件大项目
- 需要持续缓存 session 的场景

## 4. 多文件处理

`UpdateSession` 支持按 URI 维度逐个更新文件。只要这些文件属于同一包，LSP 会在语义层做包级合并分析。

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
- 精确 range 输出

返回结构遵循 LSP 风格：

```json
[
  {
    "range": {
      "start": { "line": 4, "character": 2 },
      "end": { "line": 4, "character": 14 }
    },
    "severity": 1,
    "source": "go-mini",
    "message": "类型不匹配: 无法将 String 赋值给 a (Int64)"
  }
]
```

## 6. 与执行链的关系

需要明确区分：

- LSP / IDE 查询：基于 AST 蓝图
- 运行 / 反汇编 / 持久化：基于 bytecode / prepared program

如果你的系统既要编辑又要运行，推荐双链设计：

1. 编辑时走 `lspserv`
2. 运行时走 `CompileGoCode` / `NewRuntimeByCompiled` / `NewRuntimeByBytecodeJSON`

不要把 LSP 会话缓存当作执行入口。
