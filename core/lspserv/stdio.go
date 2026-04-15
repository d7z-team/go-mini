package lspserv

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   interface{}     `json:"error,omitempty"`
}

func ServeStream(server *LSPServer, in io.Reader, out, errOut io.Writer) error {
	if server == nil {
		return errors.New("nil lsp server")
	}
	if in == nil || out == nil {
		return errors.New("nil lsp stream")
	}
	if errOut == nil {
		errOut = io.Discard
	}

	reader := bufio.NewReader(in)
	var mu sync.Mutex
	writeMessage := func(msg interface{}) {
		mu.Lock()
		defer mu.Unlock()
		body, _ := json.Marshal(msg)
		_, _ = fmt.Fprintf(out, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}

	for {
		msg, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			_, _ = fmt.Fprintf(errOut, "Error reading message: %v\n", err)
			continue
		}

		func(m *rpcMessage) {
			defer func() {
				if r := recover(); r != nil {
					_, _ = fmt.Fprintf(errOut, "LSP Panic recovered: %v\n", r)
				}
			}()
			handleMessage(server, m, writeMessage)
		}(msg)
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

func handleMessage(server *LSPServer, msg *rpcMessage, writeMessage func(interface{})) {
	switch msg.Method {
	case "initialize":
		writeMessage(rpcMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result: map[string]interface{}{
				"capabilities": map[string]interface{}{
					"textDocumentSync": 1,
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

		allDiagnostics, _ := server.UpdateSession(uri, code)
		for fURI, diags := range allDiagnostics {
			writeMessage(rpcMessage{
				JSONRPC: "2.0",
				Method:  "textDocument/publishDiagnostics",
				Params:  json.RawMessage(fmt.Sprintf(`{"uri":"%s","diagnostics":%s}`, fURI, mustMarshal(diags))),
			})
		}

	case "textDocument/completion":
		var params struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Position     Position             `json:"position"`
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
			Position     Position             `json:"position"`
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
			Position     Position             `json:"position"`
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
			Position     Position             `json:"position"`
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
