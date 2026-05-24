package lspserv

import (
	"bytes"
	"go/scanner"
	"go/token"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
)

type stubProgram struct {
	completions        []ast.CompletionItem
	definition         ast.Node
	references         []ast.Node
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

type stubAnalyzer struct {
	program     ProgramView
	errs        []error
	lastSources map[string]string
}

func (s *stubAnalyzer) AnalyzeProgramTolerant(program *ast.ProgramStmt, sources map[string]string) (ProgramView, []error) {
	s.lastSources = sources
	return s.program, s.errs
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
		Imports:      []ast.ImportSpec{{Alias: "fmt", Path: "fmt"}},
		ImportLocs:   map[string]*ast.Position{"fmt": {F: "a.go", L: 1, C: 8}},
		Types:        map[ast.Ident]ast.GoMiniType{"AliasA": "Int64"},
		TypeLocs:     map[ast.Ident]*ast.Position{"AliasA": {F: "a.go", L: 2, C: 6}},
		Functions:    map[ast.Ident]*ast.FunctionStmt{},
		Structs:      map[ast.Ident]*ast.StructStmt{},
		Variables:    map[ast.Ident]ast.Expr{},
		Constants:    map[string]string{"ConstA": "1"},
		ConstantLocs: map[string]*ast.Position{"ConstA": {F: "a.go", L: 3, C: 7}},
		Interfaces:   map[ast.Ident]*ast.InterfaceStmt{},
	}
	src := &ast.ProgramStmt{
		Imports:      []ast.ImportSpec{{Alias: "os", Path: "os"}},
		ImportLocs:   map[string]*ast.Position{"os": {F: "b.go", L: 1, C: 8}},
		Types:        map[ast.Ident]ast.GoMiniType{"AliasB": "String"},
		TypeLocs:     map[ast.Ident]*ast.Position{"AliasB": {F: "b.go", L: 2, C: 6}},
		Functions:    map[ast.Ident]*ast.FunctionStmt{},
		Structs:      map[ast.Ident]*ast.StructStmt{},
		Variables:    map[ast.Ident]ast.Expr{},
		Constants:    map[string]string{"ConstB": "2"},
		ConstantLocs: map[string]*ast.Position{"ConstB": {F: "b.go", L: 3, C: 7}},
		Interfaces:   map[ast.Ident]*ast.InterfaceStmt{},
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

func TestLSPServerPassesFileSourcesToAnalyzer(t *testing.T) {
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

func TestServeStreamInitializeAndCompletion(t *testing.T) {
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
	if !strings.Contains(out.String(), `"label":"Println"`) {
		t.Fatalf("expected completion result, got %q", out.String())
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
	if len(current) == 0 || !strings.Contains(current[0].Message, "new 第一个参数必须是类型") {
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

func TestRemoveSessionClearsDiagnosticsAndPackageState(t *testing.T) {
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

func TestServeStreamDidClosePublishesEmptyDiagnostics(t *testing.T) {
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
		t.Fatalf("expected didClose to publish empty diagnostics, got %q", out.String())
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

func TestUpdateSessionReportsMergeErrorsAsDiagnostics(t *testing.T) {
	server := NewLSPServer(&stubAnalyzer{program: &stubProgram{}})

	_, _ = server.UpdateSession("file:///workspace/a/main.go", "package main\nfunc helper() {}\n")
	diags, err := server.UpdateSession("file:///workspace/a/other.go", "package main\nfunc helper() {}\n")
	if err == nil {
		t.Fatal("expected merge error")
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
	if !strings.Contains(current[0].Message, "new 第一个参数必须是类型") {
		t.Fatalf("unexpected diagnostic message: %+v", current[0])
	}
	if current[0].Range.Start.Line != 2 {
		t.Fatalf("expected zero-based diagnostic line 2 for literal, got %+v", current[0].Range)
	}
}

func TestRangeForScannerErrorClampsEOFToVisibleRange(t *testing.T) {
	rng := rangeForScannerError("package main\nfunc main() {\n", scanner.Error{
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
	rng := rangeForScannerError("package main\nvar x = aaaa\n", scanner.Error{
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
