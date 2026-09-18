package lspserv

import (
	"bytes"
	"errors"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/gofrontend"
)

type stubProgram struct {
	completions        []ast.CompletionItem
	definition         ast.Node
	references         []ast.Node
	signatureHelp      *ast.SignatureHelpInfo
	documentSymbols    []ast.DocumentSymbolInfo
	importPaths        map[string]string
	includeDeclaration *bool
	lastFile           string
}

func (p *stubProgram) GetCompletionsAtFile(file string, line, col int) []ast.CompletionItem {
	p.lastFile = file
	return p.completions
}

func (p *stubProgram) GetHoverAtFile(file string, line, col int) *ast.HoverInfo {
	p.lastFile = file
	return nil
}

func (p *stubProgram) GetDefinitionAtFile(file string, line, col int) ast.Node {
	p.lastFile = file
	return p.definition
}

func (p *stubProgram) GetReferencesAtFile(file string, line, col int, includeDeclaration bool) []ast.Node {
	p.lastFile = file
	if p.includeDeclaration != nil {
		*p.includeDeclaration = includeDeclaration
	}
	return p.references
}

func (p *stubProgram) GetSignatureHelpAtFile(file string, line, col int) *ast.SignatureHelpInfo {
	p.lastFile = file
	return p.signatureHelp
}

func (p *stubProgram) GetDocumentSymbolsAtFile(file string) []ast.DocumentSymbolInfo {
	p.lastFile = file
	return p.documentSymbols
}

func (p *stubProgram) ResolveImportPathForPackage(alias string) string {
	return p.importPaths[alias]
}

type stubAnalyzer struct {
	program        ProgramView
	errs           []error
	diagnostics    map[string][]Diagnostic
	lastSources    map[string]string
	lastSnapshot   PackageSnapshot
	disableDerived bool
}

func (s *stubAnalyzer) AnalyzeSnapshot(snapshot PackageSnapshot, _ AnalysisOptions) (AnalysisResult, error) {
	s.lastSnapshot = snapshot
	s.lastSources = make(map[string]string, len(snapshot.Files))
	for _, file := range snapshot.Files {
		s.lastSources[file.URI] = file.Code
	}
	if s.diagnostics != nil {
		return AnalysisResult{Program: s.program, Diagnostics: cloneDiagnosticsMap(s.diagnostics)}, nil
	}
	diagnostics := make(map[string][]Diagnostic)
	var programs []*ast.ProgramStmt
	if !s.disableDerived {
		for _, file := range snapshot.Files {
			node, errs := gofrontend.NewConverter().ConvertSourceTolerant(file.URI, file.Code)
			for _, err := range errs {
				appendTestDiagnostic(diagnostics, file.URI, file.Code, err)
			}
			if prog, ok := node.(*ast.ProgramStmt); ok {
				programs = append(programs, prog)
			}
		}
		if len(programs) > 0 {
			if _, err := compiler.MergePrograms(programs); err != nil {
				appendTestDiagnostic(diagnostics, snapshot.Files[len(snapshot.Files)-1].URI, snapshot.Files[len(snapshot.Files)-1].Code, err)
			}
		}
	}
	for _, err := range s.errs {
		fallbackURI, code := "", ""
		if len(snapshot.Files) > 0 {
			fallbackURI = snapshot.Files[0].URI
			code = snapshot.Files[0].Code
		}
		appendTestDiagnostic(diagnostics, fallbackURI, code, err)
	}
	return AnalysisResult{Program: s.program, Diagnostics: diagnostics}, nil
}

func appendTestDiagnostic(diagnostics map[string][]Diagnostic, fallbackURI, code string, err error) {
	if err == nil {
		return
	}
	var scanErr scanner.Error
	if errors.As(err, &scanErr) {
		uri := scanErr.Pos.Filename
		if uri == "" {
			uri = fallbackURI
		}
		diagnostics[uri] = append(diagnostics[uri], Diagnostic{
			Range:    RangeForScannerError(code, scanErr),
			Severity: 1,
			Source:   "go-mini-syntax",
			Message:  scanErr.Msg,
		})
		return
	}
	var convertErr *gofrontend.ConvertError
	if errors.As(err, &convertErr) {
		uri := fallbackURI
		if convertErr.Pos != nil && convertErr.Pos.F != "" {
			uri = convertErr.Pos.F
		}
		diagnostics[uri] = append(diagnostics[uri], Diagnostic{
			Range:    RangeFromInternalPos(code, convertErr.Pos),
			Severity: 1,
			Source:   "go-mini-syntax",
			Message:  convertErr.Message,
		})
		return
	}
	var astErr *ast.MiniAstError
	if errors.As(err, &astErr) {
		for _, log := range astErr.Logs {
			if log.Node == nil || log.Node.GetBase() == nil || log.Node.GetBase().Loc == nil {
				continue
			}
			loc := log.Node.GetBase().Loc
			diagnostics[loc.F] = append(diagnostics[loc.F], Diagnostic{
				Range:    RangeFromInternalPos(code, loc),
				Severity: 1,
				Source:   "go-mini-semantic",
				Message:  log.Message,
			})
		}
		return
	}
	if fallbackURI != "" {
		diagnostics[fallbackURI] = append(diagnostics[fallbackURI], Diagnostic{
			Range:    RangeFromInternalPos(code, &ast.Position{F: fallbackURI, L: 1, C: 1}),
			Severity: 1,
			Source:   "go-mini",
			Message:  err.Error(),
		})
	}
}

func writeRPCMessages(dst *bytes.Buffer, bodies ...string) {
	for _, body := range bodies {
		dst.WriteString("Content-Length: ")
		dst.WriteString(strconv.Itoa(len(body)))
		dst.WriteString("\r\n\r\n")
		dst.WriteString(body)
	}
}

func TestPackageKeyForURIDistinguishesDirectories(t *testing.T) {
	keyA := packageKeyForURI("file:///workspace/a/main.go", "main")
	keyB := packageKeyForURI("file:///workspace/b/main.go", "main")
	if keyA == keyB {
		t.Fatalf("expected different package keys, got %q", keyA)
	}
}

func TestMergeProgramsIncludesImportsAndTypes(t *testing.T) {
	dest := &ast.ProgramStmt{
		Imports:       []ast.ImportSpec{{Alias: "fmt", Path: "fmt"}},
		ImportLocs:    map[string]*ast.Position{"fmt": {F: "a.go", L: 1, C: 8}},
		Types:         map[ast.Ident]ast.GoMiniType{"AliasA": "Int64"},
		TypeLocs:      map[ast.Ident]*ast.Position{"AliasA": {F: "a.go", L: 2, C: 6}},
		Functions:     map[ast.Ident]*ast.FunctionStmt{},
		Structs:       map[ast.Ident]*ast.StructStmt{},
		Variables:     map[ast.Ident]ast.Expr{},
		Constants:     map[string]string{"ConstA": "1"},
		ConstantTypes: map[string]ast.GoMiniType{"ConstA": ast.TypeInt64},
		ConstantLocs:  map[string]*ast.Position{"ConstA": {F: "a.go", L: 3, C: 7}},
		Interfaces:    map[ast.Ident]*ast.InterfaceStmt{},
	}
	src := &ast.ProgramStmt{
		Imports:       []ast.ImportSpec{{Alias: "os", Path: "os"}},
		ImportLocs:    map[string]*ast.Position{"os": {F: "b.go", L: 1, C: 8}},
		Types:         map[ast.Ident]ast.GoMiniType{"AliasB": "String"},
		TypeLocs:      map[ast.Ident]*ast.Position{"AliasB": {F: "b.go", L: 2, C: 6}},
		Functions:     map[ast.Ident]*ast.FunctionStmt{},
		Structs:       map[ast.Ident]*ast.StructStmt{},
		Variables:     map[ast.Ident]ast.Expr{},
		Constants:     map[string]string{"ConstB": "2"},
		ConstantTypes: map[string]ast.GoMiniType{"ConstB": ast.TypeInt64},
		ConstantLocs:  map[string]*ast.Position{"ConstB": {F: "b.go", L: 3, C: 7}},
		Interfaces:    map[ast.Ident]*ast.InterfaceStmt{},
	}

	merged, err := compiler.MergePrograms([]*ast.ProgramStmt{dest, src})
	if err != nil {
		t.Fatalf("MergePrograms failed: %v", err)
	}
	if len(merged.Imports) != 2 {
		t.Fatalf("expected merged imports, got %+v", merged.Imports)
	}
	if _, ok := merged.Types["AliasB"]; !ok {
		t.Fatalf("expected merged types, got %+v", merged.Types)
	}
	if merged.ImportLocs["os"].F != "b.go" || merged.TypeLocs["AliasB"].F != "b.go" || merged.ConstantLocs["ConstB"].F != "b.go" {
		t.Fatalf("expected merged declaration locations, got imports=%+v types=%+v constants=%+v", merged.ImportLocs, merged.TypeLocs, merged.ConstantLocs)
	}
}

func TestLSPServerDoesNotLeakAcrossDirectories(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})

	_, _ = server.UpdateSession("file:///workspace/a/main.go", "package main\nvar sharedA = 1\nfunc FromA() {}\n")
	_, _ = server.UpdateSession("file:///workspace/b/main.go", "package main\nfunc main() {}\n")

	items := server.GetCompletions("file:///workspace/b/main.go", 1, 4)
	for _, item := range items {
		if item.Label == "sharedA" || item.Label == "FromA" {
			t.Fatalf("unexpected cross-directory symbol leak: %+v", items)
		}
	}
}

func TestLSPServerPassesSnapshotSourcesToAnalyzer(t *testing.T) {
	analyzer := &stubAnalyzer{program: &stubProgram{}}
	server := NewLSPServer(analyzer)
	uri := "file:///workspace/a/main.go"
	code := "package main\nfunc main() {}\n"

	if _, err := server.UpdateSession(uri, code); err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}
	if got := analyzer.lastSources[uri]; got != code {
		t.Fatalf("expected analyzer source %q, got %q in %#v", code, got, analyzer.lastSources)
	}
}

func TestLSPServerSnapshotIncludesDiskPackageSiblings(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.mgo")
	helperPath := filepath.Join(dir, "helper.mgo")
	if err := os.WriteFile(mainPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helperPath, []byte("package main\nfunc Helper() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	analyzer := &stubAnalyzer{program: &stubProgram{}, disableDerived: true}
	server := NewLSPServer(analyzer)
	mainURI := fileURIForPath(mainPath)
	if _, err := server.UpdateSession(mainURI, "package main\nfunc main() { Helper() }\n"); err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}

	seen := map[string]SnapshotFile{}
	for _, file := range analyzer.lastSnapshot.Files {
		seen[file.URI] = file
	}
	if len(seen) != 2 {
		t.Fatalf("expected open file plus disk sibling in snapshot, got %+v", analyzer.lastSnapshot.Files)
	}
	helper := seen[fileURIForPath(helperPath)]
	if helper.Open || !strings.Contains(helper.Code, "Helper") {
		t.Fatalf("expected helper from disk snapshot, got %+v", helper)
	}
}

func TestRemoveSessionFallsBackToDiskFile(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.mgo")
	if err := os.WriteFile(mainPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})
	uri := fileURIForPath(mainPath)
	first, _ := server.UpdateSession(uri, "package main\nfunc main() {\n")
	if len(first[uri]) == 0 {
		t.Fatalf("expected diagnostics for dirty open overlay, got %+v", first)
	}

	updates := server.RemoveSession(uri)
	if current, ok := updates[uri]; !ok || len(current) != 0 {
		t.Fatalf("expected diagnostics to clear from clean disk fallback, got %+v", updates)
	}
	if server.packageForURI(uri) == nil {
		t.Fatal("expected package to remain queryable from disk file")
	}
}

func TestRefreshWorkspaceFilesReanalyzesDiskSibling(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.mgo")
	helperPath := filepath.Join(dir, "helper.mgo")
	if err := os.WriteFile(mainPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	analyzer := &stubAnalyzer{program: &stubProgram{}, disableDerived: true}
	server := NewLSPServer(analyzer)
	mainURI := fileURIForPath(mainPath)
	if _, err := server.UpdateSession(mainURI, "package main\nfunc main() {}\n"); err != nil {
		t.Fatalf("UpdateSession failed: %v", err)
	}
	if err := os.WriteFile(helperPath, []byte("package main\nfunc Helper() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := server.RefreshWorkspaceFiles([]string{fileURIForPath(helperPath)}); err != nil {
		t.Fatalf("RefreshWorkspaceFiles failed: %v", err)
	}

	found := false
	for _, file := range analyzer.lastSnapshot.Files {
		if file.URI == fileURIForPath(helperPath) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected watched disk sibling in refreshed snapshot, got %+v", analyzer.lastSnapshot.Files)
	}
}

func TestServeStreamInitializeAdvertisesCapabilitiesAndCompletion(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{
		program: &stubProgram{
			completions: []ast.CompletionItem{{Label: "Println", Kind: "func", Type: "function(String) Void"}},
		},
	})

	initBody := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`
	openBody := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///workspace/a/main.go","text":"package main\nfunc main() {}\n"}}}`
	completionBody := `{"jsonrpc":"2.0","id":2,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///workspace/a/main.go"},"position":{"line":1,"character":1}}}`

	var in bytes.Buffer
	writeRPCMessages(&in, initBody, openBody, completionBody)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := ServeStream(server, &in, &out, &errOut); err != nil {
		t.Fatalf("ServeStream returned error: %v", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), `"completionProvider"`) {
		t.Fatalf("expected initialize response, got %q", out.String())
	}
	if !strings.Contains(out.String(), `"change":1`) {
		t.Fatalf("expected full text sync capability, got %q", out.String())
	}
	if !strings.Contains(out.String(), `"signatureHelpProvider"`) || !strings.Contains(out.String(), `"semanticTokensProvider"`) || !strings.Contains(out.String(), `"codeActionProvider"`) || !strings.Contains(out.String(), `"didChangeWatchedFiles"`) {
		t.Fatalf("expected extended initialize capabilities, got %q", out.String())
	}
	if !strings.Contains(out.String(), `"label":"Println"`) {
		t.Fatalf("expected completion result, got %q", out.String())
	}
}

func TestServeStreamRejectsInvalidContentLength(t *testing.T) {
	cases := []string{
		"Content-Length: -1\r\n\r\n",
		"Content-Length: nope\r\n\r\n",
	}
	for _, input := range cases {
		var out bytes.Buffer
		var errOut bytes.Buffer
		err := ServeStream(NewLSPServer(&stubAnalyzer{program: &stubProgram{}}), strings.NewReader(input), &out, &errOut)
		if err == nil {
			t.Fatalf("ServeStream(%q) returned nil error", input)
		}
	}
}

func TestDebouncedDiagnosticsPublishOnlyLatestChange(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})
	server.diagnosticDelay = 10 * time.Millisecond
	uri := "file:///workspace/a/main.mgo"

	var mu sync.Mutex
	var published []map[string][]Diagnostic
	server.setDiagnosticPublisher(func(updates map[string][]Diagnostic) {
		mu.Lock()
		defer mu.Unlock()
		published = append(published, cloneDiagnosticsMap(updates))
	})
	defer server.setDiagnosticPublisher(nil)
	defer server.stopPendingDiagnostics()

	server.updateSessionDebounced(uri, "package main\nfunc main() {\n")
	server.updateSessionDebounced(uri, `package main
func main() {
	_ = new("Int64")
}
`)

	select {
	case <-time.After(80 * time.Millisecond):
	}

	mu.Lock()
	defer mu.Unlock()
	if len(published) != 1 {
		t.Fatalf("expected one debounced diagnostics publish, got %+v", published)
	}
	current := published[0][uri]
	if len(current) == 0 || !strings.Contains(current[0].Message, "new first argument must be a type") {
		t.Fatalf("expected latest diagnostics only, got %+v", published[0])
	}
}

func TestFlushDiagnosticsPublishesPendingChangeImmediately(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})
	server.diagnosticDelay = time.Hour
	uri := "file:///workspace/a/main.mgo"

	server.updateSessionDebounced(uri, "package main\nfunc main() {\n")
	updates, err := server.flushDiagnostics(uri)
	if err != nil {
		t.Fatalf("flush diagnostics failed: %v", err)
	}
	if len(updates[uri]) == 0 {
		t.Fatalf("expected flushed diagnostics, got %+v", updates)
	}

	time.Sleep(20 * time.Millisecond)
	updates, err = server.flushDiagnostics(uri)
	if err != nil {
		t.Fatalf("second flush diagnostics failed: %v", err)
	}
	if len(updates) != 0 {
		t.Fatalf("expected pending timer to be canceled by first flush, got %+v", updates)
	}
}

func TestRemoveSessionCancelsPendingDiagnostics(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})
	server.diagnosticDelay = 10 * time.Millisecond
	uri := "file:///workspace/a/main.mgo"

	var mu sync.Mutex
	published := 0
	server.setDiagnosticPublisher(func(map[string][]Diagnostic) {
		mu.Lock()
		defer mu.Unlock()
		published++
	})
	defer server.setDiagnosticPublisher(nil)

	server.updateSessionDebounced(uri, "package main\nfunc main() {\n")
	updates := server.RemoveSession(uri)
	if diags, ok := updates[uri]; !ok || len(diags) != 0 {
		t.Fatalf("expected close diagnostics clear, got %+v", updates)
	}
	time.Sleep(40 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if published != 0 {
		t.Fatalf("expected pending diagnostics to be canceled, got %d publishes", published)
	}
}

func TestQueryRefreshesAnalysisDuringDiagnosticDebounce(t *testing.T) {
	analyzer := &stubAnalyzer{
		program: &stubProgram{
			completions: []ast.CompletionItem{{Label: "Fresh", Kind: "func", Type: "function() Void"}},
		},
	}
	server := NewLSPServer(analyzer)
	server.diagnosticDelay = time.Hour
	uri := "file:///workspace/a/main.mgo"
	code := "package main\nfunc Fresh() {}\n"

	server.updateSessionDebounced(uri, code)
	items := server.GetCompletions(uri, 1, 4)
	if len(items) == 0 || items[0].Label != "Fresh" {
		t.Fatalf("expected fresh completions during debounce, got %+v", items)
	}
	if got := analyzer.lastSources[uri]; got != code {
		t.Fatalf("expected query to analyze latest source %q, got %q", code, got)
	}
	server.stopPendingDiagnostics()
}

func TestUpdateSessionClearsPreviousDiagnostics(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{
		program: &stubProgram{},
		errs: []error{
			&ast.MiniAstError{
				Logs: []ast.Logs{
					{
						Message: "first error",
						Node: &ast.IdentifierExpr{
							BaseNode: ast.BaseNode{Loc: &ast.Position{F: "file:///workspace/a/main.go", L: 1, C: 1, EL: 1, EC: 2}},
						},
					},
				},
			},
		},
	})

	first, _ := server.UpdateSession("file:///workspace/a/main.go", "package main\n")
	if len(first["file:///workspace/a/main.go"]) == 0 {
		t.Fatalf("expected diagnostics on first update, got %+v", first)
	}

	server.executor = &stubAnalyzer{program: &stubProgram{}}
	second, _ := server.UpdateSession("file:///workspace/a/main.go", "package main\n")
	diags, ok := second["file:///workspace/a/main.go"]
	if !ok {
		t.Fatalf("expected clearing diagnostics entry, got %+v", second)
	}
	if len(diags) != 0 {
		t.Fatalf("expected diagnostics to clear, got %+v", diags)
	}
}

func TestUpdateSessionClearsPreviousSyntaxDiagnostics(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})

	first, _ := server.UpdateSession("file:///workspace/a/main.mgo", "package main\nfunc main() {\n    aaaa\n")
	initial, ok := first["file:///workspace/a/main.mgo"]
	if !ok || len(initial) == 0 {
		t.Fatalf("expected syntax diagnostics on first update, got %+v", first)
	}

	second, _ := server.UpdateSession("file:///workspace/a/main.mgo", "package main\nfunc main() {\n    aaaa\n}\n")
	diags, ok := second["file:///workspace/a/main.mgo"]
	if !ok {
		t.Fatalf("expected clearing diagnostics entry, got %+v", second)
	}
	if len(diags) != 0 {
		t.Fatalf("expected syntax diagnostics to clear, got %+v", diags)
	}
}

func TestRemoveSessionClearsDiagnosticsWhenNoDiskFile(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})
	uri := "file:///workspace/a/main.mgo"

	first, _ := server.UpdateSession(uri, "package main\nfunc main() {\n    aaaa\n")
	if len(first[uri]) == 0 {
		t.Fatalf("expected diagnostics on first update, got %+v", first)
	}

	updates := server.RemoveSession(uri)
	diags, ok := updates[uri]
	if !ok {
		t.Fatalf("expected clearing diagnostics entry, got %+v", updates)
	}
	if len(diags) != 0 {
		t.Fatalf("expected diagnostics to clear, got %+v", diags)
	}
	if server.packageForURI(uri) != nil {
		t.Fatal("expected removed file to leave no package session")
	}
}

func TestServeStreamDidCloseClearsDiagnosticsWithoutDiskFallback(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})
	uri := "file:///workspace/a/main.mgo"

	openBody := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"` + uri + `","text":"package main\nfunc main() {\n    aaaa\n"}}}`
	closeBody := `{"jsonrpc":"2.0","method":"textDocument/didClose","params":{"textDocument":{"uri":"` + uri + `"}}}`

	var in bytes.Buffer
	writeRPCMessages(&in, openBody, closeBody)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := ServeStream(server, &in, &out, &errOut); err != nil {
		t.Fatalf("ServeStream returned error: %v", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), `"diagnostics":[]`) {
		t.Fatalf("expected didClose without disk fallback to clear diagnostics, got %q", out.String())
	}
}

func TestServeStreamDidSaveFlushesPendingDiagnostics(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})
	server.diagnosticDelay = time.Hour
	uri := "file:///workspace/a/main.mgo"

	openBody := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"` + uri + `","text":"package main\nfunc main() {\n    aaaa\n"}}}`
	saveBody := `{"jsonrpc":"2.0","method":"textDocument/didSave","params":{"textDocument":{"uri":"` + uri + `"}}}`

	var in bytes.Buffer
	writeRPCMessages(&in, openBody, saveBody)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := ServeStream(server, &in, &out, &errOut); err != nil {
		t.Fatalf("ServeStream returned error: %v", err)
	}
	if errOut.Len() != 0 {
		t.Fatalf("expected empty stderr, got %q", errOut.String())
	}
	if !strings.Contains(out.String(), `"method":"textDocument/publishDiagnostics"`) {
		t.Fatalf("expected didSave to flush diagnostics, got %q", out.String())
	}
}

func TestServeStreamShutdownExitAndErrors(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})
	unknownBody := `{"jsonrpc":"2.0","id":1,"method":"workspace/unknown","params":{}}`
	invalidParamsBody := `{"jsonrpc":"2.0","id":2,"method":"textDocument/hover","params":"bad"}`
	shutdownBody := `{"jsonrpc":"2.0","id":3,"method":"shutdown","params":null}`
	afterShutdownBody := `{"jsonrpc":"2.0","id":4,"method":"textDocument/completion","params":{"textDocument":{"uri":"file:///workspace/a/main.go"},"position":{"line":1,"character":1}}}`
	exitBody := `{"jsonrpc":"2.0","method":"exit"}`

	var in bytes.Buffer
	writeRPCMessages(&in, unknownBody, invalidParamsBody, shutdownBody, afterShutdownBody, exitBody)

	var out bytes.Buffer
	var errOut bytes.Buffer
	if err := ServeStream(server, &in, &out, &errOut); err != nil {
		t.Fatalf("ServeStream returned error: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, `"code":-32601`) {
		t.Fatalf("expected method-not-found response, got %q", got)
	}
	if !strings.Contains(got, `"code":-32602`) {
		t.Fatalf("expected invalid-params response, got %q", got)
	}
	if !strings.Contains(got, `"id":3`) {
		t.Fatalf("expected shutdown response, got %q", got)
	}
	if !strings.Contains(got, `"id":4`) || !strings.Contains(got, `"code":-32600`) {
		t.Fatalf("expected post-shutdown request rejection, got %q", got)
	}
}

func TestGetReferencesHonorsIncludeDeclaration(t *testing.T) {
	uri := "file:///workspace/a/main.go"
	include := true
	ref := &ast.IdentifierExpr{
		BaseNode: ast.BaseNode{Loc: &ast.Position{F: uri, L: 2, C: 6, EL: 2, EC: 10}},
		Name:     "name",
	}
	program := &stubProgram{
		references:         []ast.Node{ref},
		includeDeclaration: &include,
	}
	server := NewLSPServer(&stubAnalyzer{
		program: program,
	})

	_, _ = server.UpdateSession(uri, "package main\nfunc main() {}\n")
	locs := server.GetReferences(uri, 1, 5, false)
	if include {
		t.Fatal("expected includeDeclaration=false to reach ProgramView")
	}
	if program.lastFile != uri {
		t.Fatalf("expected references query to include URI %q, got %q", uri, program.lastFile)
	}
	if len(locs) != 1 || locs[0].URI != uri {
		t.Fatalf("expected one reference location, got %+v", locs)
	}
}

func TestLSPServerReturnsSignatureSymbolsTokensAndImportAction(t *testing.T) {
	uri := "file:///workspace/a/main.go"
	program := &stubProgram{
		signatureHelp: &ast.SignatureHelpInfo{Signatures: []ast.SignatureInformation{{Label: "Helper(v Int64)"}}},
		documentSymbols: []ast.DocumentSymbolInfo{{
			Name:         "Helper",
			Kind:         "func",
			Loc:          &ast.Position{F: uri, L: 2, C: 1, EL: 2, EC: 7},
			SelectionLoc: &ast.Position{F: uri, L: 2, C: 1, EL: 2, EC: 7},
		}},
		importPaths: map[string]string{"filepath": "path/filepath"},
	}
	server := NewLSPServer(&stubAnalyzer{program: program})
	_, _ = server.UpdateSession(uri, "package main\nfunc Helper(v Int64) {}\nfunc main() { Helper(1) }\n")

	if help := server.GetSignatureHelp(uri, 1, 50); help == nil || len(help.Signatures) != 1 {
		t.Fatalf("expected signature help from ProgramView, got %+v", help)
	}
	if symbols := server.GetDocumentSymbols(uri); len(symbols) != 1 || symbols[0].Name != "Helper" {
		t.Fatalf("expected document symbols from ProgramView, got %+v", symbols)
	}
	if tokens := server.GetSemanticTokens(uri); tokens == nil || len(tokens.Data) == 0 {
		t.Fatalf("expected semantic tokens, got %+v", tokens)
	}
	actions := server.GetCodeActions(uri, []Diagnostic{{Message: "package filepath resolved but not imported"}})
	if len(actions) != 1 || !strings.Contains(actions[0].Edit.Changes[uri][0].NewText, `import "path/filepath"`) {
		t.Fatalf("expected missing import code action, got %+v", actions)
	}
}

func TestUpdateSessionReportsMergeErrorsAsDiagnostics(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})

	_, _ = server.UpdateSession("file:///workspace/a/main.go", "package main\nfunc helper() {}\n")
	diags, err := server.UpdateSession("file:///workspace/a/other.go", "package main\nfunc helper() {}\n")
	if err != nil {
		t.Fatalf("merge diagnostics should not surface as server error: %v", err)
	}
	found := false
	for _, current := range diags {
		for _, diag := range current {
			if strings.Contains(diag.Message, "duplicate function definition") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected diagnostics for merge error, got %+v", diags)
	}
}

func TestUpdateSessionReportsConverterErrorsAtSourceRange(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})

	diags, _ := server.UpdateSession("file:///workspace/a/main.mgo", `package main
func main() {
	_ = new("Int64")
}
`)
	current := diags["file:///workspace/a/main.mgo"]
	if len(current) == 0 {
		t.Fatalf("expected converter diagnostic, got %+v", diags)
	}
	if !strings.Contains(current[0].Message, "new first argument must be a type") {
		t.Fatalf("unexpected diagnostic message: %+v", current[0])
	}
	if current[0].Range.Start.Line != 2 {
		t.Fatalf("expected zero-based diagnostic line 2 for literal, got %+v", current[0].Range)
	}
}

func TestRangeForScannerErrorClampsEOFToVisibleRange(t *testing.T) {
	rng := RangeForScannerError("package main\nfunc main() {\n", scanner.Error{
		Pos: token.Position{Line: 3, Column: 2},
		Msg: "expected '}', found 'EOF'",
	})
	if rng.Start.Line != 1 {
		t.Fatalf("expected EOF to clamp to last real line, got %+v", rng)
	}
	if rng.End.Character <= rng.Start.Character {
		t.Fatalf("expected visible highlight width, got %+v", rng)
	}
}

func TestRangeForScannerErrorUsesTokenWidth(t *testing.T) {
	rng := RangeForScannerError("package main\nvar x = aaaa\n", scanner.Error{
		Pos: token.Position{Line: 2, Column: 9},
		Msg: "expected ';', found aaaa",
	})
	if got := rng.End.Character - rng.Start.Character; got != 4 {
		t.Fatalf("expected token width 4, got %+v", rng)
	}
}

func TestRangeFromInternalPosUsesUTF16Columns(t *testing.T) {
	code := "package main\nvar 名 = bad\n"
	rng := RangeFromInternalPos(code, &ast.Position{F: "snippet", L: 2, C: 11, EL: 2, EC: 14})
	if rng.Start.Line != 1 {
		t.Fatalf("expected zero-based line 1, got %+v", rng)
	}
	if rng.Start.Character != 8 || rng.End.Character != 11 {
		t.Fatalf("expected UTF-16 columns 8..11, got %+v", rng)
	}
}
