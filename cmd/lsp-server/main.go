package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/lspserv"
)

// 这是一个极简的 LSP JSON-RPC 实现，不引入重型框架以保持轻量。
// 它通过标准输入输出与 IDE 通信。

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   interface{}     `json:"error,omitempty"`
}

func main() {
	executor := engine.NewMiniExecutor()
	// 注入标准库
	executor.InjectStandardLibraries()

	server := lspserv.NewLSPServer(executor)
	reader := bufio.NewReader(os.Stdin)

	for {
		msg, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Fprintf(os.Stderr, "Error reading message: %v\n", err)
			continue
		}

		handleMessage(server, msg)
	}
}

func readMessage(r *bufio.Reader) (*rpcMessage, error) {
	var contentLength int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "Content-Length:") {
			_, _ = fmt.Sscanf(line, "Content-Length: %d", &contentLength)
		}
	}

	if contentLength == 0 {
		return nil, errors.New("invalid content length")
	}

	body := make([]byte, contentLength)
	_, err := io.ReadFull(r, body)
	if err != nil {
		return nil, err
	}

	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func writeMessage(msg interface{}) {
	body, _ := json.Marshal(msg)
	fmt.Printf("Content-Length: %d\r\n\r\n%s", len(body), body)
}

func handleMessage(server *lspserv.LSPServer, msg *rpcMessage) {
	switch msg.Method {
	case "initialize":
		writeMessage(rpcMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result: map[string]interface{}{
				"capabilities": map[string]interface{}{
					"textDocumentSync": 1, // Full
					"completionProvider": map[string]interface{}{
						"resolveProvider":   false,
						"triggerCharacters": []string{"."},
					},
					"hoverProvider":      true,
					"definitionProvider": true,
					"referencesProvider": true,
				},
			},
		})

	case "textDocument/didOpen", "textDocument/didChange":
		var params struct {
			TextDocument struct {
				URI  string `json:"uri"`
				Text string `json:"text"`
			} `json:"textDocument"`
			ContentChanges []struct {
				Text string `json:"text"`
			} `json:"contentChanges"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return
		}

		uri := params.TextDocument.URI
		code := params.TextDocument.Text
		if len(params.ContentChanges) > 0 {
			code = params.ContentChanges[0].Text
		}

		diagnostics, _ := server.UpdateSession(uri, code)

		// 发送异步诊断推送
		writeMessage(rpcMessage{
			JSONRPC: "2.0",
			Method:  "textDocument/publishDiagnostics",
			Params:  json.RawMessage(fmt.Sprintf(`{"uri":"%s","diagnostics":%s}`, uri, mustMarshal(diagnostics))),
		})

	case "textDocument/completion":
		var params struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Position     lspserv.Position     `json:"position"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return
		}

		items := server.GetCompletions(params.TextDocument.URI, params.Position.Line, params.Position.Character)
		writeMessage(rpcMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  items,
		})

	case "textDocument/hover":
		var params struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Position     lspserv.Position     `json:"position"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return
		}

		hover := server.GetHover(params.TextDocument.URI, params.Position.Line, params.Position.Character)
		writeMessage(rpcMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  hover,
		})

	case "textDocument/definition":
		var params struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Position     lspserv.Position     `json:"position"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return
		}

		locs := server.GetDefinition(params.TextDocument.URI, params.Position.Line, params.Position.Character)
		writeMessage(rpcMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  locs,
		})

	case "textDocument/references":
		var params struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Position     lspserv.Position     `json:"position"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			return
		}

		locs := server.GetReferences(params.TextDocument.URI, params.Position.Line, params.Position.Character)
		writeMessage(rpcMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result:  locs,
		})
	}
}

func mustMarshal(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
