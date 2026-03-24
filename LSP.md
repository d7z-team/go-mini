# Go-Mini LSP 集成指南 (Web IDE / Monaco Editor)

本文档介绍如何将 `go-mini` 的代码提示与诊断功能无缝集成到基于 Web 的在线编辑器中（如 [Monaco Editor](https://microsoft.github.io/monaco-editor/)）。

为了简化架构并消除后端的有状态管理负担，我们推荐使用**无状态通信模式**：即每次触发代码提示或检查时，前端直接将**所有的当前代码和光标位置**发送给后端进行即时计算。

---

## 1. 后端 API 实现 (Go)

你不需要启动 `cmd/lsp-server`，而是直接调用 `core/lspserv` 提供的引擎能力，将其包装成你现有的 HTTP 接口。

### 示例后端代码 (基于 net/http)

```go
package main

import (
	"encoding/json"
	"net/http"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/lspserv"
)

type LSPRequest struct {
	Code string `json:"code"`
	Line int    `json:"line"` // 0-based
	Char int    `json:"char"` // 0-based
}

func main() {
	// 1. 初始化包含你所有 FFI 的全局 Executor
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()

	// 2. 包装为 LSP Server 服务
	lsp := lspserv.NewLSPServer(executor)

	http.HandleFunc("/api/complete", func(w http.ResponseWriter, r *http.Request) {
		var req LSPRequest
		json.NewDecoder(r.Body).Decode(&req)

		// 无状态执行：由于我们不使用增量同步，每次都生成一个随机的 URI 供此次请求使用
		uri := "virtual://temp.mini"

		// 第一步：解析当前代码并更新临时会话
		// UpdateSession 会返回当前文件的诊断信息（包括语法错误和语义错误）
		diags, _ := lsp.UpdateSession(uri, req.Code)

		// 第二步：获取补全建议 (注意：lspServer 要求 0-based 坐标)
		items := lsp.GetCompletions(uri, req.Line, req.Char)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"completions": items,
			"diagnostics": diags,
		})
	})
```

---

## 3. 诊断信息 (Diagnostics) 与错误提示

`go-mini` 的 LSP 引擎现在支持高精度的诊断信息反馈：

1.  **即时语法错误**：即使代码无法运行（如括号不匹配），引擎也会返回 `go-mini-syntax` 来源的错误。
2.  **包级语义校验**：在多文件模式下，引擎会跨文件分析类型冲突、未定义变量等。
3.  **精准的文件过滤**：即使包内其他文件有错，`UpdateSession` 也只会返回属于当前 `uri` 的错误，避免编辑器界面出现“错位”的波浪线。
4.  **符合 LSP 标准的坐标**：
    *   **起始/结束位置**：所有错误均包含完整的 `Range`（Start 和 End）。
    *   **容错处理**：对于没有明确范围的错误（如运行时 Panic），引擎会自动推导合理的显示范围（如 `Start + 1`），确保编辑器始终能渲染出波浪线。

### 诊断信息结构示例

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

---

## 4. 多文件支持（包级合并）

如果你的 Web IDE 允许用户创建多个文件，并且你希望提供跨文件的代码提示：

在发给后端的请求中，你可以将所有同目录（同包）下的代码用换行符拼接起来，或者在你的 `LSPRequest` 结构体中支持传入一个文件数组。

在后端，只需针对同一个 `uri` 前缀调用多次 `UpdateSession` 即可：

```go
// 后端接收到一个包含多个文件的请求时：
for filename, code := range req.Files {
    // lspserv 内部会自动识别 package 声明，并将同名 package 的符号合并
    lsp.UpdateSession("virtual://project/"+filename, code)
}

// 最后基于用户当前光标所在的文件获取提示
items := lsp.GetCompletions("virtual://project/"+req.CurrentFile, req.Line, req.Char)
```

---

## 4. 有状态通信模式 (WebSocket / 增量同步)

如果你的应用具有长连接特性（如使用 WebSocket 或长轮询），并且需要处理超大型项目，可以采用有状态的通信模式。这种模式与标准的 LSP 协议类似，利用 `LSPServer` 的内部缓存来避免每次都重新解析完整的代码。

在这种模式下，后端会维护一个 `LSPServer` 单例，并通过 URI 来区分和缓存不同的文件。

### 示例后端代码 (WebSocket 概念)

```go
package main

import (
	"encoding/json"
	"net/http"
	"github.com/gorilla/websocket" // 需要安装此依赖

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/lspserv"
)

var upgrader = websocket.Upgrader{
    CheckOrigin: func(r *http.Request) bool { return true },
}

// 定义客户端发送的消息格式
type WSMessage struct {
    Type   string `json:"type"`             // "update", "complete", "hover"
    URI    string `json:"uri"`              // 文件的唯一标识符
    Code   string `json:"code,omitempty"`   // 文件内容 (仅 update 时需要)
    Line   int    `json:"line,omitempty"`   // 行号 (0-based)
    Char   int    `json:"char,omitempty"`   // 列号 (0-based)
}

func main() {
	executor := engine.NewMiniExecutor()
	executor.InjectStandardLibraries()
    // 全局共享的 LSP Server 实例
	lsp := lspserv.NewLSPServer(executor)

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, _ := upgrader.Upgrade(w, r, nil)
		defer conn.Close()

		for {
			var msg WSMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}

			switch msg.Type {
			case "update":
                // 客户端在文件打开或内容发生变化时发送
                // lspserv 会自动覆盖该 URI 的旧内容并重新编译
				diags, _ := lsp.UpdateSession(msg.URI, msg.Code)
				conn.WriteJSON(map[string]interface{}{
                    "type": "diagnostics", 
                    "uri": msg.URI, 
                    "data": diags,
                })

			case "complete":
                // 客户端仅发送 URI 和光标位置，后端直接从缓存中读取 AST
				items := lsp.GetCompletions(msg.URI, msg.Line, msg.Char)
				conn.WriteJSON(map[string]interface{}{
                    "type": "completion", 
                    "data": items,
                })
                
            case "hover":
                hover := lsp.GetHover(msg.URI, msg.Line, msg.Char)
                conn.WriteJSON(map[string]interface{}{
                    "type": "hover",
                    "data": hover,
                })
			}
		}
	})

	http.ListenAndServe(":8080", nil)
}
```

### 两种模式的对比

| 特性 | 无状态模式 (HTTP POST) | 有状态模式 (WebSocket Session) |
| :--- | :--- | :--- |
| **适用场景** | Serverless、无状态微服务、简单网页编辑器 | 复杂的云 IDE、大型多文件项目、长连接应用 |
| **性能开销** | 每次请求都需要全量解析代码，CPU 开销较大 | 利用 `LSPServer` 内部缓存，仅在代码变更时解析，响应更快 |
| **内存占用** | 请求结束后释放，内存占用极低 | 需要在内存中常驻所有打开文件的 AST 缓存，占用相对较高 |
| **多文件处理** | 每次请求需将关联文件一并发送 | 客户端只需发送当前正在编辑的变更文件即可 |

