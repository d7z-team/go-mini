package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	protocol "go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

type retryDiagnosticsClient struct {
	protocol.UnimplementedClient
	calls int
}

type recordingDiagnosticsClient struct {
	protocol.UnimplementedClient
	published []uri.URI
}

func (client *recordingDiagnosticsClient) PublishDiagnostics(_ context.Context, params *protocol.PublishDiagnosticsParams) error {
	client.published = append(client.published, params.URI)
	return nil
}

func (client *retryDiagnosticsClient) PublishDiagnostics(context.Context, *protocol.PublishDiagnosticsParams) error {
	client.calls++
	if client.calls == 1 {
		return errors.New("publish failed")
	}
	return nil
}

func TestProtocolServerStandardLifecycle(t *testing.T) {
	set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: "package main\nfunc main() {}\n"}}}})
	if err != nil {
		t.Fatal(err)
	}
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	done := make(chan error, 1)
	go func() {
		done <- ServeProtocol(context.Background(), serverConn, serverConn, func(context.Context, WorkspaceOptions) (Workspace, error) {
			return Workspace{RootPath: "/workspace", ModulePath: "example", Sources: set}, nil
		})
	}()
	if err := clientConn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(clientConn)
	writeProtocolFrame(clientConn, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"workspaceFolders": []map[string]any{{"uri": "file:///workspace", "name": "workspace"}}, "capabilities": map[string]any{}, "initializationOptions": map[string]any{"tags": []string{"editor"}}}})
	initialize := readProtocolResponse(t, reader)
	if initialize["error"] != nil {
		t.Fatalf("initialize = %#v", initialize)
	}
	writeProtocolFrame(clientConn, map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
	writeProtocolFrame(clientConn, map[string]any{"jsonrpc": "2.0", "id": 2, "method": "shutdown"})
	shutdown := readProtocolResponse(t, reader)
	if result, exists := shutdown["result"]; !exists || result != nil {
		t.Fatalf("shutdown response = %#v", shutdown)
	}
	writeProtocolFrame(clientConn, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("LSP server did not exit")
	}
}

func TestProtocolServerSelectsWorkspaceRoot(t *testing.T) {
	set, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: "package main\n"}}}})
	if err != nil {
		t.Fatal(err)
	}
	initialize := func(params *protocol.InitializeParams) (string, error) {
		var loadedRoot string
		server := NewProtocolServer(func(_ context.Context, options WorkspaceOptions) (Workspace, error) {
			root := options.Root
			loadedRoot = root
			return Workspace{RootPath: root, ModulePath: "example", Sources: set}, nil
		})
		_, err := server.Initialize(context.Background(), params)
		return loadedRoot, err
	}
	root, err := initialize(&protocol.InitializeParams{
		WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{WorkspaceFolders: protocol.NewNullable([]protocol.WorkspaceFolder{{URI: uri.File("/folder-root"), Name: "folder"}})},
	})
	if err != nil || root != "/folder-root" {
		t.Fatalf("workspaceFolders root = %q, %v", root, err)
	}
	params := &protocol.InitializeParams{}
	rootURI := uri.File("/uri-root")
	params.RootURI = &rootURI
	params.RootPath = protocol.NewNullable("/legacy-root")
	if root, err := initialize(params); err != nil || root != "/uri-root" {
		t.Fatalf("rootUri root = %q, %v", root, err)
	}
	params.RootURI = nil
	if root, err := initialize(params); err != nil || root != "/legacy-root" {
		t.Fatalf("rootPath root = %q, %v", root, err)
	}
	params.WorkspaceFolders = protocol.NewNullable([]protocol.WorkspaceFolder{{URI: uri.File("/a")}, {URI: uri.File("/b")}})
	if root, err := initialize(params); err == nil || root != "" {
		t.Fatalf("multiple roots accepted: %q, %v", root, err)
	}
	if _, err := initialize(&protocol.InitializeParams{}); err == nil {
		t.Fatal("initialize accepted a missing workspace root")
	}
}

func TestProtocolServerNegotiatesClientCapabilities(t *testing.T) {
	edit := WorkspaceEdit{DocumentChanges: []TextDocumentEdit{{
		TextDocument: VersionedTextDocumentIdentifier{URI: "file:///workspace/main.mgo", Version: 3},
		Edits:        []TextEdit{{NewText: "value"}},
	}}}
	plain := wireWorkspaceEdit(edit, protocol.ClientCapabilities{})
	if len(plain.Changes) != 1 || len(plain.DocumentChanges) != 0 {
		t.Fatalf("plain workspace edit = %#v", plain)
	}
	documentChanges := true
	versionedCapabilities := protocol.ClientCapabilities{Workspace: &protocol.WorkspaceClientCapabilities{WorkspaceEdit: &protocol.WorkspaceEditClientCapabilities{DocumentChanges: &documentChanges}}}
	versioned := wireWorkspaceEdit(edit, versionedCapabilities)
	if len(versioned.Changes) != 0 || len(versioned.DocumentChanges) != 1 {
		t.Fatalf("versioned workspace edit = %#v", versioned)
	}
	plainJSON, err := json.Marshal(serverCapabilities(protocol.ClientCapabilities{}))
	if err != nil || !strings.Contains(string(plainJSON), `"codeActionProvider":true`) {
		t.Fatalf("plain code action capability = %s, %v", plainJSON, err)
	}
	literalCapabilities := protocol.ClientCapabilities{TextDocument: &protocol.TextDocumentClientCapabilities{CodeAction: &protocol.CodeActionClientCapabilities{
		CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{CodeActionKind: protocol.ClientCodeActionKindOptions{ValueSet: []protocol.CodeActionKind{protocol.CodeActionKindQuickFix}}},
	}}}
	literalJSON, err := json.Marshal(serverCapabilities(literalCapabilities))
	if err != nil || !strings.Contains(string(literalJSON), `"codeActionProvider":{"codeActionKinds"`) {
		t.Fatalf("literal code action capability = %s, %v", literalJSON, err)
	}
}

func TestProtocolServerRejectsLifecycleBeforeInitialize(t *testing.T) {
	server := NewProtocolServer(nil)
	if _, err := server.Initialize(context.Background(), nil); err == nil {
		t.Fatal("initialize accepted nil params")
	}
	if _, err := server.Hover(context.Background(), nil); err == nil {
		t.Fatal("hover accepted nil params")
	}
	if err := server.Initialized(context.Background(), nil); err == nil {
		t.Fatal("initialized notification succeeded before initialize")
	}
	if err := server.Shutdown(context.Background()); err == nil {
		t.Fatal("shutdown succeeded before initialize")
	}
}

func TestProtocolServerRecordsDiagnosticsOnlyAfterPublish(t *testing.T) {
	engine, documentURI := testEngine(t, "package main\nfunc main() {}\n")
	server := NewProtocolServer(nil)
	server.engine, server.initialized = engine, true
	client := &retryDiagnosticsClient{}
	ctx := protocol.WithClient(context.Background(), client)
	if err := server.publishChanged(ctx); err == nil {
		t.Fatal("first diagnostic publish succeeded")
	}
	if _, exists := server.published[documentURI]; exists {
		t.Fatal("failed diagnostic publish was recorded")
	}
	if err := server.publishChanged(ctx); err != nil {
		t.Fatal(err)
	}
	if server.published[documentURI] != engine.Snapshot().ResultID(documentURI) || client.calls != 2 {
		t.Fatalf("published state = %q after %d calls", server.published[documentURI], client.calls)
	}
}

func TestProtocolServerPublishesOnlyEditableDocuments(t *testing.T) {
	sources, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{
		{ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"dependency\"\nfunc main() { dependency.Value() }\n"}}},
		{ModulePath: "dependency", Files: []source.File{{Path: "value.mgo", Text: "package dependency\nfunc Value() {}\n"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	documents, err := workspace.NewMemorySourceSet([]workspace.SourcePackage{{
		ModulePath: "example", Files: []source.File{{Path: "main.mgo", Text: "package main\nimport \"dependency\"\nfunc main() { dependency.Value() }\n"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := NewEngine(Config{Root: "example", Sources: sources, Documents: documents, URI: func(_, _ string) DocumentURI {
		return "file:///workspace/main.mgo"
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := NewProtocolServer(nil)
	server.engine, server.initialized = engine, true
	client := &recordingDiagnosticsClient{}
	if err := server.publishChanged(protocol.WithClient(context.Background(), client)); err != nil {
		t.Fatal(err)
	}
	if len(client.published) != 1 || client.published[0] != "file:///workspace/main.mgo" {
		t.Fatalf("published diagnostics = %#v", client.published)
	}
}

func TestProtocolServerCancelsInitializeRequest(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- ServeProtocol(context.Background(), serverConn, serverConn, func(ctx context.Context, _ WorkspaceOptions) (Workspace, error) {
			close(started)
			<-ctx.Done()
			return Workspace{}, ctx.Err()
		})
	}()
	if err := clientConn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(clientConn)
	writeProtocolFrame(clientConn, map[string]any{"jsonrpc": "2.0", "id": 7, "method": "initialize", "params": map[string]any{"workspaceFolders": []map[string]any{{"uri": "file:///workspace", "name": "workspace"}}, "capabilities": map[string]any{}}})
	<-started
	writeProtocolFrame(clientConn, map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": 7}})
	response := readProtocolResponse(t, reader)
	errorValue, ok := response["error"].(map[string]any)
	if !ok || errorValue["code"] != float64(-32800) {
		t.Fatalf("cancel response = %#v", response)
	}
	writeProtocolFrame(clientConn, map[string]any{"jsonrpc": "2.0", "method": "exit"})
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("exit before shutdown returned success")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("LSP server did not exit")
	}
}

func writeProtocolFrame(writer io.Writer, value any) {
	data, _ := json.Marshal(value)
	fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n", len(data))
	_, _ = writer.Write(data)
}

func readProtocolResponse(t *testing.T, reader *bufio.Reader) map[string]any {
	t.Helper()
	payload, err := readProtocolFrame(reader)
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]any
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func readProtocolFrame(reader *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length < 0 {
		return nil, errors.New("missing Content-Length")
	}
	payload := make([]byte, length)
	_, err := io.ReadFull(reader, payload)
	return payload, err
}
