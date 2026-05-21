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

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type streamState struct {
	shutdown bool
}

var (
	errMissingTextDocumentURI = errors.New("missing textDocument.uri")
	jsonNull                  = json.RawMessage("null")
)

type publishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Diagnostics []Diagnostic `json:"diagnostics"`
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

	state := &streamState{}
	for {
		msg, err := readMessage(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			_, _ = fmt.Fprintf(errOut, "Error reading message: %v\n", err)
			continue
		}
		shouldExit := false
		func(m *rpcMessage) {
			defer func() {
				if r := recover(); r != nil {
					_, _ = fmt.Fprintf(errOut, "LSP Panic recovered: %v\n", r)
				}
			}()
			shouldExit = handleMessage(server, m, state, writeMessage, errOut)
		}(msg)
		if shouldExit {
			return nil
		}
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
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg rpcMessage
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func handleMessage(server *LSPServer, msg *rpcMessage, state *streamState, writeMessage func(interface{}), errOut io.Writer) bool {
	if state != nil && state.shutdown && msg.Method != "exit" {
		if msg.ID != nil {
			writeMessage(rpcMessage{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Error:   rpcError{Code: -32600, Message: "server is shut down"},
			})
		}
		return false
	}

	switch msg.Method {
	case "initialize":
		writeMessage(rpcMessage{
			JSONRPC: "2.0",
			ID:      msg.ID,
			Result: map[string]interface{}{
				"capabilities": map[string]interface{}{
					"textDocumentSync": map[string]interface{}{
						"openClose": true,
						"change":    1,
					},
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
		return false

	case "initialized", "$/cancelRequest":
		return false

	case "shutdown":
		if state != nil {
			state.shutdown = true
		}
		if msg.ID != nil {
			writeMessage(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: jsonNull})
		}
		return false

	case "exit":
		return true

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
			writeInvalidParams(msg, writeMessage, err)
			return false
		}
		uri := params.TextDocument.URI
		if strings.TrimSpace(uri) == "" {
			writeInvalidParams(msg, writeMessage, errMissingTextDocumentURI)
			return false
		}
		code := params.TextDocument.Text
		if len(params.ContentChanges) > 0 {
			code = params.ContentChanges[0].Text
		}
		allDiagnostics, err := server.UpdateSession(uri, code)
		if err != nil {
			_, _ = fmt.Fprintf(errOut, "Error updating session for %s: %v\n", uri, err)
		}
		for fURI, diags := range allDiagnostics {
			payload, marshalErr := json.Marshal(publishDiagnosticsParams{
				URI:         fURI,
				Diagnostics: diags,
			})
			if marshalErr != nil {
				_, _ = fmt.Fprintf(errOut, "Error marshaling diagnostics for %s: %v\n", fURI, marshalErr)
				continue
			}
			writeMessage(rpcMessage{
				JSONRPC: "2.0",
				Method:  "textDocument/publishDiagnostics",
				Params:  json.RawMessage(payload),
			})
		}
		return false

	case "textDocument/didClose":
		var params struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			writeInvalidParams(msg, writeMessage, err)
			return false
		}
		if strings.TrimSpace(params.TextDocument.URI) == "" {
			writeInvalidParams(msg, writeMessage, errMissingTextDocumentURI)
			return false
		}
		for fURI, diags := range server.RemoveSession(params.TextDocument.URI) {
			payload, marshalErr := json.Marshal(publishDiagnosticsParams{
				URI:         fURI,
				Diagnostics: diags,
			})
			if marshalErr != nil {
				_, _ = fmt.Fprintf(errOut, "Error marshaling diagnostics for %s: %v\n", fURI, marshalErr)
				continue
			}
			writeMessage(rpcMessage{
				JSONRPC: "2.0",
				Method:  "textDocument/publishDiagnostics",
				Params:  json.RawMessage(payload),
			})
		}
		return false

	case "textDocument/completion":
		var params struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Position     Position             `json:"position"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			writeInvalidParams(msg, writeMessage, err)
			return false
		}
		if strings.TrimSpace(params.TextDocument.URI) == "" {
			writeInvalidParams(msg, writeMessage, errMissingTextDocumentURI)
			return false
		}
		items := server.GetCompletions(params.TextDocument.URI, params.Position.Line, params.Position.Character)
		writeMessage(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: items})
		return false

	case "textDocument/hover":
		var params struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Position     Position             `json:"position"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			writeInvalidParams(msg, writeMessage, err)
			return false
		}
		if strings.TrimSpace(params.TextDocument.URI) == "" {
			writeInvalidParams(msg, writeMessage, errMissingTextDocumentURI)
			return false
		}
		hover := server.GetHover(params.TextDocument.URI, params.Position.Line, params.Position.Character)
		writeMessage(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: hover})
		return false

	case "textDocument/definition":
		var params struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Position     Position             `json:"position"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			writeInvalidParams(msg, writeMessage, err)
			return false
		}
		if strings.TrimSpace(params.TextDocument.URI) == "" {
			writeInvalidParams(msg, writeMessage, errMissingTextDocumentURI)
			return false
		}
		locs := server.GetDefinition(params.TextDocument.URI, params.Position.Line, params.Position.Character)
		writeMessage(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: locs})
		return false

	case "textDocument/references":
		var params struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Position     Position             `json:"position"`
			Context      struct {
				IncludeDeclaration bool `json:"includeDeclaration"`
			} `json:"context"`
		}
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			writeInvalidParams(msg, writeMessage, err)
			return false
		}
		if strings.TrimSpace(params.TextDocument.URI) == "" {
			writeInvalidParams(msg, writeMessage, errMissingTextDocumentURI)
			return false
		}
		locs := server.GetReferences(params.TextDocument.URI, params.Position.Line, params.Position.Character, params.Context.IncludeDeclaration)
		writeMessage(rpcMessage{JSONRPC: "2.0", ID: msg.ID, Result: locs})
		return false
	default:
		if msg.ID != nil {
			writeMessage(rpcMessage{
				JSONRPC: "2.0",
				ID:      msg.ID,
				Error:   rpcError{Code: -32601, Message: "method not found: " + msg.Method},
			})
		}
		return false
	}
}

func writeInvalidParams(msg *rpcMessage, writeMessage func(interface{}), err error) {
	if msg == nil || msg.ID == nil {
		return
	}
	writeMessage(rpcMessage{
		JSONRPC: "2.0",
		ID:      msg.ID,
		Error:   rpcError{Code: -32602, Message: "invalid params: " + err.Error()},
	})
}
