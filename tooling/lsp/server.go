package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"

	"go.lsp.dev/jsonrpc2"
	protocol "go.lsp.dev/protocol"
	"go.lsp.dev/uri"

	"github.com/d7z-team/mini-go/compiler/language"
	"github.com/d7z-team/mini-go/compiler/target"
	"github.com/d7z-team/mini-go/compiler/workspace"
)

// Workspace describes one loaded source workspace. Loading remains a host
// concern so the language server can also be embedded with a virtual source set.
type Workspace struct {
	Locations  *workspace.SourceLocations
	RootPath   string
	ModulePath string
	Sources    workspace.SourceSet
	Documents  workspace.SourceSet
}

type WorkspaceOptions struct {
	Root    string
	Module  string                      `json:"module"`
	Sources []workspace.DirectorySource `json:"sources"`
	Tags    []string                    `json:"tags"`
}
type WorkspaceLoader func(context.Context, WorkspaceOptions) (Workspace, error)

type ProtocolServer struct {
	protocol.UnimplementedServer

	mu                     sync.RWMutex
	updates                sync.Mutex
	publication            sync.Mutex
	loader                 WorkspaceLoader
	loadOptions            WorkspaceOptions
	engine                 *Engine
	workspace              Workspace
	target                 target.Target
	capabilities           protocol.ClientCapabilities
	initialized            bool
	initializing           bool
	initializeTried        bool
	shutdown               bool
	exitErr                error
	published              map[DocumentURI]string
	workspacePublished     string
	workspaceDiagnosticURI uri.URI
	exit                   chan struct{}
	exitOnce               sync.Once
	initializeDone         chan struct{}
}

func NewProtocolServer(loader WorkspaceLoader) *ProtocolServer {
	return &ProtocolServer{
		loader:         loader,
		published:      make(map[DocumentURI]string),
		exit:           make(chan struct{}),
		initializeDone: make(chan struct{}),
	}
}

// ServeProtocol serves one LSP connection using the standard Content-Length
// JSON-RPC transport and the generated LSP protocol model.
func ServeProtocol(ctx context.Context, input io.Reader, output io.Writer, loader WorkspaceLoader) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stream := jsonrpc2.NewHeaderStream(splitReadWriteCloser{Reader: input, Writer: output})
	server := NewProtocolServer(loader)
	_, conn, _ := protocol.NewServer(ctx, server, stream)
	select {
	case <-server.exit:
		closeErr := conn.Close()
		server.mu.RLock()
		exitErr := server.exitErr
		server.mu.RUnlock()
		return errors.Join(exitErr, closeErr)
	case <-conn.Done():
		return conn.Err()
	case <-ctx.Done():
		_ = conn.Close()
		return ctx.Err()
	}
}

type splitReadWriteCloser struct {
	io.Reader
	io.Writer
}

func (stream splitReadWriteCloser) Close() error {
	if closer, ok := stream.Reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (s *ProtocolServer) Initialize(ctx context.Context, params *protocol.InitializeParams) (*protocol.InitializeResult, error) {
	if params == nil {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "missing initialize params")
	}
	s.mu.Lock()
	if s.initializeTried || s.shutdown {
		s.mu.Unlock()
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidRequest, "server is already initialized")
	}
	s.initializeTried, s.initializing = true, true
	defer close(s.initializeDone)
	defer func() {
		s.mu.Lock()
		s.initializing = false
		s.mu.Unlock()
	}()
	s.mu.Unlock()
	if s.loader == nil {
		return nil, jsonrpc2.NewError(jsonrpc2.InternalError, "nil workspace loader")
	}
	root := ""
	if folders, ok := params.WorkspaceFolders.Get(); ok && len(folders) != 0 {
		if len(folders) != 1 {
			return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "only one workspace folder is supported")
		}
		root = folders[0].URI.FsPath()
	}
	if strings.TrimSpace(root) == "" && params.RootURI != nil { //nolint:staticcheck // LSP clients may still use the specified rootUri fallback.
		root = params.RootURI.FsPath() //nolint:staticcheck // Retain the protocol fallback when workspaceFolders is absent.
	}
	if strings.TrimSpace(root) == "" {
		root, _ = params.RootPath.Get() //nolint:staticcheck // Older LSP clients supply only rootPath.
	}
	if strings.TrimSpace(root) == "" {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, "initialize requires a workspace root")
	}
	options := WorkspaceOptions{Root: root}
	if len(params.InitializationOptions) != 0 {
		if err := json.Unmarshal(params.InitializationOptions, &options); err != nil {
			return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, err.Error())
		}
	}
	options.Root = root
	loaded, err := s.loader(ctx, options)
	if err != nil {
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		return nil, jsonrpc2.NewError(jsonrpc2.InternalError, err.Error())
	}
	buildTarget, err := target.Normalize(target.Target{Tags: options.Tags})
	if err != nil {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidParams, err.Error())
	}
	engine, err := newWorkspaceEngine(ctx, loaded, buildTarget)
	if err != nil {
		if ctx.Err() != nil {
			return nil, protocol.ErrRequestCancelled
		}
		return nil, jsonrpc2.NewError(jsonrpc2.InternalError, err.Error())
	}
	s.mu.Lock()
	s.workspace, s.target, s.engine = loaded, buildTarget, engine
	s.loadOptions = options
	s.capabilities = params.Capabilities
	s.initialized = true
	s.mu.Unlock()
	return &protocol.InitializeResult{Capabilities: serverCapabilities(params.Capabilities), ServerInfo: protocol.ServerInfo{Name: "mini-go-lsp", Version: protocol.NewOptional("2")}}, nil
}

func (loaded Workspace) documentURI(module, path string) DocumentURI {
	if file, ok := loaded.Locations.File(module, path); ok {
		return DocumentURI(uri.File(file).String())
	}
	if loaded.Locations != nil {
		return DocumentURI("mini-go://" + module + "/" + path)
	}
	return DocumentURI(uri.File(filepath.Join(loaded.RootPath, filepath.FromSlash(path))).String())
}

func newWorkspaceEngine(ctx context.Context, loaded Workspace, buildTarget target.Target) (*Engine, error) {
	return NewEngineContext(ctx, Config{Root: loaded.ModulePath, Target: buildTarget, Sources: loaded.Sources, Documents: loaded.Documents, URI: loaded.documentURI})
}

func serverCapabilities(client protocol.ClientCapabilities) protocol.ServerCapabilities {
	openClose, includeSaveText, incremental := true, false, protocol.TextDocumentSyncKindIncremental
	capabilities := protocol.ServerCapabilities{
		PositionEncoding: protocol.PositionEncodingKindUTF16,
		TextDocumentSync: &protocol.TextDocumentSyncOptions{
			OpenClose: &openClose,
			Change:    &incremental,
			Save:      &protocol.SaveOptions{IncludeText: &includeSaveText},
		},
		HoverProvider:                   protocol.Boolean(true),
		DefinitionProvider:              protocol.Boolean(true),
		ReferencesProvider:              protocol.Boolean(true),
		TypeDefinitionProvider:          protocol.Boolean(true),
		ImplementationProvider:          protocol.Boolean(true),
		DocumentHighlightProvider:       protocol.Boolean(true),
		CompletionProvider:              &protocol.CompletionOptions{ResolveProvider: &openClose, TriggerCharacters: []string{"."}},
		SignatureHelpProvider:           &protocol.SignatureHelpOptions{TriggerCharacters: []string{"(", ","}},
		DocumentSymbolProvider:          protocol.Boolean(true),
		WorkspaceSymbolProvider:         protocol.Boolean(true),
		DocumentFormattingProvider:      protocol.Boolean(true),
		DocumentRangeFormattingProvider: protocol.Boolean(true),
		DocumentOnTypeFormattingProvider: protocol.DocumentOnTypeFormattingOptions{
			FirstTriggerCharacter: "}",
			MoreTriggerCharacter:  []string{"\n"},
		},
		RenameProvider:         &protocol.RenameOptions{PrepareProvider: &openClose},
		FoldingRangeProvider:   protocol.Boolean(true),
		SelectionRangeProvider: protocol.Boolean(true),
		SemanticTokensProvider: &protocol.SemanticTokensOptions{
			Legend: protocol.SemanticTokensLegend{TokenTypes: SemanticTokenTypes, TokenModifiers: SemanticTokenModifiers},
			Full:   protocol.Boolean(true),
			Range:  protocol.Boolean(true),
		},
		DiagnosticProvider: &protocol.DiagnosticOptions{InterFileDependencies: true, WorkspaceDiagnostics: false},
	}
	if client.TextDocument != nil && client.TextDocument.CodeAction != nil && len(client.TextDocument.CodeAction.CodeActionLiteralSupport.CodeActionKind.ValueSet) != 0 {
		capabilities.CodeActionProvider = &protocol.CodeActionOptions{
			ResolveProvider: &openClose,
			CodeActionKinds: []protocol.CodeActionKind{
				protocol.CodeActionKindQuickFix,
				protocol.CodeActionKindSource,
			},
		}
	} else {
		capabilities.CodeActionProvider = protocol.Boolean(true)
	}
	return capabilities
}

func (s *ProtocolServer) Initialized(context.Context, *protocol.InitializedParams) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.initialized {
		return jsonrpc2.NewError(jsonrpc2.Code(protocol.ErrorCodesServerNotInitialized), "server is not initialized")
	}
	if s.shutdown {
		return jsonrpc2.NewError(jsonrpc2.InvalidRequest, "server is shut down")
	}
	return nil
}

func (s *ProtocolServer) Shutdown(ctx context.Context) error {
	s.mu.RLock()
	initializing := s.initializing
	initialized := s.initialized
	s.mu.RUnlock()
	if !initializing && !initialized {
		return jsonrpc2.NewError(jsonrpc2.Code(protocol.ErrorCodesServerNotInitialized), "server is not initialized")
	}
	select {
	case <-s.initializeDone:
	case <-ctx.Done():
		return protocol.ErrRequestCancelled
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialized {
		return jsonrpc2.NewError(jsonrpc2.Code(protocol.ErrorCodesServerNotInitialized), "server is not initialized")
	}
	if s.shutdown {
		return jsonrpc2.NewError(jsonrpc2.InvalidRequest, "server is already shut down")
	}
	s.shutdown = true
	return nil
}

func (s *ProtocolServer) Exit(context.Context) error {
	s.mu.Lock()
	if !s.shutdown {
		s.exitErr = errors.New("LSP exit received before shutdown")
	}
	s.mu.Unlock()
	s.exitOnce.Do(func() { close(s.exit) })
	return nil
}

func (s *ProtocolServer) engineForRequest() (*Engine, error) {
	if !s.initialized || s.engine == nil {
		return nil, jsonrpc2.NewError(jsonrpc2.Code(protocol.ErrorCodesServerNotInitialized), "server is not initialized")
	}
	if s.shutdown {
		return nil, jsonrpc2.NewError(jsonrpc2.InvalidRequest, "server is shut down")
	}
	return s.engine, nil
}

func (s *ProtocolServer) DidOpen(ctx context.Context, params *protocol.DidOpenTextDocumentParams) error {
	if params == nil {
		return jsonrpc2.NewError(jsonrpc2.InvalidParams, "missing didOpen params")
	}
	return s.changeDocuments(ctx, func() ([]language.DocumentUpdate, error) {
		identity, err := s.identity(DocumentURI(params.TextDocument.URI.String()))
		if err != nil {
			return nil, err
		}
		return []language.DocumentUpdate{{Operation: "open", Identity: identity, Version: int(params.TextDocument.Version), Text: params.TextDocument.Text}}, nil
	})
}

func (s *ProtocolServer) DidChange(ctx context.Context, params *protocol.DidChangeTextDocumentParams) error {
	if params == nil {
		return jsonrpc2.NewError(jsonrpc2.InvalidParams, "missing didChange params")
	}
	changes := make([]ContentChange, 0, len(params.ContentChanges))
	for _, change := range params.ContentChanges {
		switch value := change.(type) {
		case *protocol.TextDocumentContentChangePartial:
			selected := engineRange(value.Range)
			changes = append(changes, ContentChange{Range: &selected, Text: value.Text})
		case *protocol.TextDocumentContentChangeWholeDocument:
			changes = append(changes, ContentChange{Text: value.Text})
		default:
			return jsonrpc2.NewError(jsonrpc2.InvalidParams, "unsupported document change")
		}
	}
	return s.changeDocuments(ctx, func() ([]language.DocumentUpdate, error) {
		return []language.DocumentUpdate{{Operation: "change", Identity: DocumentIdentity{URI: DocumentURI(params.TextDocument.URI.String())}, Version: int(params.TextDocument.Version), Changes: changes}}, nil
	})
}

func (s *ProtocolServer) DidSave(ctx context.Context, params *protocol.DidSaveTextDocumentParams) error {
	if params == nil {
		return jsonrpc2.NewError(jsonrpc2.InvalidParams, "missing didSave params")
	}
	return s.reload(ctx)
}

func (s *ProtocolServer) DidClose(ctx context.Context, params *protocol.DidCloseTextDocumentParams) error {
	if params == nil {
		return jsonrpc2.NewError(jsonrpc2.InvalidParams, "missing didClose params")
	}
	return s.changeDocuments(ctx, func() ([]language.DocumentUpdate, error) {
		return []language.DocumentUpdate{{Operation: "close", Identity: DocumentIdentity{URI: DocumentURI(params.TextDocument.URI.String())}}}, nil
	})
}

func (s *ProtocolServer) DidChangeWatchedFiles(ctx context.Context, params *protocol.DidChangeWatchedFilesParams) error {
	if params == nil {
		return jsonrpc2.NewError(jsonrpc2.InvalidParams, "missing didChangeWatchedFiles params")
	}
	return s.reload(ctx)
}

func (s *ProtocolServer) changeDocuments(ctx context.Context, prepare func() ([]language.DocumentUpdate, error)) error {
	s.updates.Lock()
	defer s.updates.Unlock()
	s.mu.RLock()
	engine, err := s.engineForRequest()
	var changes []language.DocumentUpdate
	if err == nil {
		changes, err = prepare()
	}
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	candidate := engine.Fork()
	if err := candidate.ApplyDocuments(changes); err != nil {
		return err
	}
	if err := candidate.Analyze(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return errors.New("server shut down")
	}
	s.engine = candidate
	s.mu.Unlock()
	return s.publishChanged(ctx)
}

func (s *ProtocolServer) reload(ctx context.Context) error {
	s.updates.Lock()
	defer s.updates.Unlock()
	s.mu.RLock()
	engine, err := s.engineForRequest()
	options, buildTarget := s.loadOptions, s.target
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	loaded, err := s.loader(ctx, options)
	if err != nil {
		return err
	}
	candidate, err := engine.ReplaceWorkspace(ctx, Config{Root: loaded.ModulePath, Target: buildTarget, Sources: loaded.Sources, Documents: loaded.Documents, URI: loaded.documentURI})
	if err != nil {
		return err
	}
	if err := candidate.Analyze(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	if s.shutdown {
		s.mu.Unlock()
		return errors.New("server shut down")
	}
	s.workspace, s.engine = loaded, candidate
	s.mu.Unlock()
	return s.publishChanged(ctx)
}

func (s *ProtocolServer) identity(documentURI DocumentURI) (DocumentIdentity, error) {
	parsed, err := uri.ParseStrict(string(documentURI))
	if err != nil || !parsed.IsFile() {
		return DocumentIdentity{}, errors.New("document URI must be an absolute file URI")
	}
	name := parsed.FsPath()
	if s.workspace.Locations != nil {
		if location, ok := s.workspace.Locations.Identity(name); ok {
			if _, editable, err := s.workspace.Documents.Package(location.ModulePath); err != nil {
				return DocumentIdentity{}, err
			} else if !editable {
				return DocumentIdentity{}, errors.New("source is read-only")
			}
			return DocumentIdentity{URI: documentURI, ModulePath: location.ModulePath, Path: location.Path}, nil
		}
	}
	relative, err := filepath.Rel(s.workspace.RootPath, name)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return DocumentIdentity{}, errors.New("document is outside workspace")
	}
	for _, document := range s.engine.Snapshot().Documents() {
		if document.Identity.URI == documentURI {
			return document.Identity, nil
		}
	}
	dir := filepath.ToSlash(filepath.Dir(relative))
	modulePath := s.workspace.ModulePath
	if dir != "." {
		modulePath += "/" + dir
	}
	return DocumentIdentity{URI: documentURI, ModulePath: modulePath, Path: filepath.ToSlash(relative)}, nil
}

func (s *ProtocolServer) publishChanged(ctx context.Context) error {
	s.publication.Lock()
	defer s.publication.Unlock()
	client, ok := protocol.ClientFromContext(ctx)
	if !ok {
		return nil
	}
	s.mu.RLock()
	snapshot, root := s.engine.Snapshot(), s.workspace.RootPath
	s.mu.RUnlock()
	documents := snapshot.Documents()
	workspaceDiagnostics := snapshot.WorkspaceDiagnostics()
	for documentURI := range s.published {
		if _, exists := documents[documentURI]; exists && snapshot.IsEditable(documentURI) {
			continue
		}
		if err := client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{URI: uri.URI(documentURI), Diagnostics: []protocol.Diagnostic{}}); err != nil {
			return err
		}
		delete(s.published, documentURI)
	}
	for documentURI, document := range documents {
		if !snapshot.IsEditable(documentURI) {
			continue
		}
		resultID := snapshot.ResultID(documentURI)
		if s.published[documentURI] == resultID {
			continue
		}
		diagnostics := wireDiagnostics(snapshot.Diagnostics(documentURI))
		version := int32(document.Version)
		if err := client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{URI: uri.URI(documentURI), Version: protocol.NewOptional(version), Diagnostics: diagnostics}); err != nil {
			return err
		}
		s.published[documentURI] = resultID
	}
	encoded, err := json.Marshal(workspaceDiagnostics)
	if err != nil {
		return err
	}
	identity := string(encoded)
	if identity != s.workspacePublished && (len(workspaceDiagnostics) != 0 || s.workspacePublished != "") {
		diagnostics := make([]Diagnostic, 0, len(workspaceDiagnostics))
		for _, diagnostic := range workspaceDiagnostics {
			message := diagnostic.Message
			if diagnostic.ModulePath != "" {
				message = diagnostic.ModulePath + ": " + message
			}
			diagnostics = append(diagnostics, Diagnostic{Severity: 1, Code: string(diagnostic.Code), Source: "mini-go", Message: message})
		}
		s.workspaceDiagnosticURI = uri.File(root)
		if s.workspaceDiagnosticURI != "" {
			if err := client.PublishDiagnostics(ctx, &protocol.PublishDiagnosticsParams{URI: s.workspaceDiagnosticURI, Diagnostics: wireDiagnostics(diagnostics)}); err != nil {
				return err
			}
		}
		if len(diagnostics) != 0 {
			if err := client.ShowMessage(ctx, &protocol.ShowMessageParams{Type: protocol.MessageTypeError, Message: "Mini-Go workspace: " + diagnostics[0].Message}); err != nil {
				return err
			}
		}
		s.workspacePublished = identity
	}
	return nil
}
