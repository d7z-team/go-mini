package lspserv

import (
	"errors"
	"fmt"
	"go/scanner"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type ProgramView interface {
	GetCompletionsAt(line, col int) []ast.CompletionItem
	GetHoverAt(line, col int) *ast.HoverInfo
	GetDefinitionAt(line, col int) ast.Node
	GetReferencesAt(line, col int) []ast.Node
}

type Analyzer interface {
	AnalyzeProgramTolerant(program *ast.ProgramStmt) (ProgramView, []error)
}

type LSPServer struct {
	executor Analyzer

	mu       sync.RWMutex
	files    map[string]*fileSession
	packages map[string]*packageState
}

type fileSession struct {
	uri      string
	pkgKey   string
	pkgName  string
	code     string
	version  uint64
	modified uint64
}

type packageState struct {
	key string

	mu sync.RWMutex

	files                map[string]*fileSession
	combined             ProgramView
	publishedDiagnostics map[string][]Diagnostic
	version              uint64
}

type parsedFile struct {
	uri         string
	program     *ast.ProgramStmt
	diagnostics []Diagnostic
}

var scannerFoundTokenPattern = regexp.MustCompile(`found\s+('?[^']+'?|\S+)$`)

func NewLSPServer(e Analyzer) *LSPServer {
	return &LSPServer{
		executor: e,
		files:    make(map[string]*fileSession),
		packages: make(map[string]*packageState),
	}
}

func (s *LSPServer) UpdateSession(uri, code string) (map[string][]Diagnostic, error) {
	pkgName := detectPackageName(uri, code)
	pkgKey := packageKeyForURI(uri, pkgName)

	s.mu.Lock()
	file := s.files[uri]
	if file == nil {
		file = &fileSession{uri: uri}
		s.files[uri] = file
	}
	oldPkgKey := file.pkgKey
	file.version++
	file.modified = file.version
	file.pkgKey = pkgKey
	file.pkgName = pkgName
	file.code = code

	currentPkg := s.ensurePackageLocked(pkgKey)
	currentPkg.mu.Lock()
	currentPkg.files[uri] = cloneFileSession(file)
	currentPkg.version++
	currentPkg.mu.Unlock()

	var oldPkg *packageState
	if oldPkgKey != "" && oldPkgKey != pkgKey {
		oldPkg = s.packages[oldPkgKey]
		if oldPkg != nil {
			oldPkg.mu.Lock()
			delete(oldPkg.files, uri)
			oldPkg.version++
			shouldDelete := len(oldPkg.files) == 0
			oldPkg.mu.Unlock()
			if shouldDelete {
				delete(s.packages, oldPkgKey)
			}
		}
	}
	s.mu.Unlock()

	result := make(map[string][]Diagnostic)
	if oldPkg != nil {
		oldDiagnostics, _ := s.rebuildPackage(oldPkg)
		mergeDiagnostics(result, oldDiagnostics)
	}
	currentDiagnostics, err := s.rebuildPackage(currentPkg)
	mergeDiagnostics(result, currentDiagnostics)
	return result, err
}

func (s *LSPServer) ensurePackageLocked(pkgKey string) *packageState {
	if pkg := s.packages[pkgKey]; pkg != nil {
		return pkg
	}
	pkg := &packageState{
		key:                  pkgKey,
		files:                make(map[string]*fileSession),
		publishedDiagnostics: make(map[string][]Diagnostic),
	}
	s.packages[pkgKey] = pkg
	return pkg
}

func cloneFileSession(in *fileSession) *fileSession {
	if in == nil {
		return nil
	}
	cloned := *in
	return &cloned
}

func (s *LSPServer) rebuildPackage(pkg *packageState) (map[string][]Diagnostic, error) {
	if pkg == nil {
		return nil, nil
	}

	files, version := snapshotPackageFiles(pkg)
	if len(files) == 0 {
		return finalizeDiagnostics(pkg, nil), nil
	}

	parsed := make([]parsedFile, 0, len(files))
	diagnostics := make(map[string][]Diagnostic)
	for _, file := range files {
		item := parseFileForLSP(file)
		parsed = append(parsed, item)
		if len(item.diagnostics) > 0 {
			diagnostics[item.uri] = append(diagnostics[item.uri], item.diagnostics...)
		}
	}

	combined, mergeURI, mergeErr := mergeParsedPrograms(parsed)
	if mergeErr != nil {
		if mergeURI == "" && len(files) > 0 {
			mergeURI = files[len(files)-1].uri
		}
		if mergeURI != "" {
			diagnostics[mergeURI] = append(diagnostics[mergeURI], Diagnostic{
				Range:    FromInternalPos(&ast.Position{F: mergeURI, L: 1, C: 1}),
				Severity: 1,
				Source:   "go-mini",
				Message:  mergeErr.Error(),
			})
		}
		return finalizeDiagnostics(pkg, diagnostics), mergeErr
	}
	if combined == nil {
		return finalizeDiagnostics(pkg, diagnostics), nil
	}

	prog, errs := s.executor.AnalyzeProgramTolerant(combined)
	for _, err := range errs {
		appendAnalysisDiagnostics(diagnostics, err)
	}
	return finalizePackageState(pkg, version, prog, diagnostics), nil
}

func snapshotPackageFiles(pkg *packageState) ([]*fileSession, uint64) {
	pkg.mu.RLock()
	defer pkg.mu.RUnlock()

	files := make([]*fileSession, 0, len(pkg.files))
	for _, file := range pkg.files {
		files = append(files, cloneFileSession(file))
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].uri < files[j].uri
	})
	return files, pkg.version
}

func parseFileForLSP(file *fileSession) parsedFile {
	result := parsedFile{uri: file.uri}
	converter := ffigo.NewGoToASTConverter()
	node, errs := converter.ConvertSourceTolerant(file.uri, file.code)
	for _, err := range errs {
		var scanErr scanner.Error
		if errors.As(err, &scanErr) {
			result.diagnostics = append(result.diagnostics, Diagnostic{
				Range:    rangeForScannerError(file.code, scanErr),
				Severity: 1,
				Source:   "go-mini-syntax",
				Message:  scanErr.Msg,
			})
			continue
		}
		if err != nil {
			result.diagnostics = append(result.diagnostics, Diagnostic{
				Range:    FromInternalPos(&ast.Position{F: file.uri, L: 1, C: 1}),
				Severity: 1,
				Source:   "go-mini-syntax",
				Message:  err.Error(),
			})
		}
	}
	if prog, ok := node.(*ast.ProgramStmt); ok {
		result.program = prog
	}
	return result
}

func mergeParsedPrograms(parsed []parsedFile) (*ast.ProgramStmt, string, error) {
	var programs []*ast.ProgramStmt
	var lastURI string
	for _, item := range parsed {
		if item.program == nil {
			continue
		}
		programs = append(programs, item.program)
		lastURI = item.uri
	}
	if len(programs) == 0 {
		return nil, "", nil
	}
	combined, err := compiler.MergePrograms(programs)
	if err != nil {
		return nil, lastURI, err
	}
	return combined, "", nil
}

func appendAnalysisDiagnostics(diagnostics map[string][]Diagnostic, err error) {
	if err == nil {
		return
	}
	if astErr, ok := err.(*ast.MiniAstError); ok {
		for _, log := range astErr.Logs {
			if log.Node == nil || log.Node.GetBase() == nil {
				continue
			}
			loc := log.Node.GetBase().Loc
			if loc == nil || loc.F == "" {
				continue
			}
			diagnostics[loc.F] = append(diagnostics[loc.F], Diagnostic{
				Range:    FromInternalPos(loc),
				Severity: 1,
				Source:   "go-mini",
				Message:  log.Message,
			})
		}
		return
	}
	var vme *runtime.VMError
	if errors.As(err, &vme) && len(vme.Frames) > 0 {
		f := vme.Frames[0]
		diagnostics[f.Filename] = append(diagnostics[f.Filename], Diagnostic{
			Range:    FromInternalPos(&ast.Position{F: f.Filename, L: f.Line, C: f.Column}),
			Severity: 1,
			Source:   "go-mini-runtime",
			Message:  vme.Message,
		})
	}
}

func finalizePackageState(pkg *packageState, version uint64, combined ProgramView, diagnostics map[string][]Diagnostic) map[string][]Diagnostic {
	pkg.mu.Lock()
	defer pkg.mu.Unlock()
	if version == pkg.version {
		pkg.combined = combined
	}
	return diffDiagnosticsLocked(pkg, diagnostics)
}

func finalizeDiagnostics(pkg *packageState, diagnostics map[string][]Diagnostic) map[string][]Diagnostic {
	pkg.mu.Lock()
	defer pkg.mu.Unlock()
	return diffDiagnosticsLocked(pkg, diagnostics)
}

func diffDiagnosticsLocked(pkg *packageState, diagnostics map[string][]Diagnostic) map[string][]Diagnostic {
	if diagnostics == nil {
		diagnostics = make(map[string][]Diagnostic)
	}

	updates := make(map[string][]Diagnostic)
	for uri, old := range pkg.publishedDiagnostics {
		if _, ok := diagnostics[uri]; !ok && len(old) > 0 {
			updates[uri] = []Diagnostic{}
		}
	}
	for uri, next := range diagnostics {
		if !diagnosticSlicesEqual(pkg.publishedDiagnostics[uri], next) {
			updates[uri] = append([]Diagnostic(nil), next...)
		}
	}

	pkg.publishedDiagnostics = cloneDiagnosticsMap(diagnostics)
	return updates
}

func cloneDiagnosticsMap(in map[string][]Diagnostic) map[string][]Diagnostic {
	out := make(map[string][]Diagnostic, len(in))
	for uri, diags := range in {
		out[uri] = append([]Diagnostic(nil), diags...)
	}
	return out
}

func diagnosticSlicesEqual(a, b []Diagnostic) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Range != b[i].Range || a[i].Severity != b[i].Severity || a[i].Source != b[i].Source || a[i].Message != b[i].Message {
			return false
		}
		if len(a[i].RelatedInformation) != len(b[i].RelatedInformation) {
			return false
		}
		for j := range a[i].RelatedInformation {
			if a[i].RelatedInformation[j] != b[i].RelatedInformation[j] {
				return false
			}
		}
	}
	return true
}

func (s *LSPServer) packageForURI(uri string) *packageState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	file := s.files[uri]
	if file == nil {
		return nil
	}
	return s.packages[file.pkgKey]
}

func mergeDiagnostics(dst, src map[string][]Diagnostic) {
	for uri, diags := range src {
		dst[uri] = diags
	}
}

func detectPackageName(uri, code string) string {
	converter := ffigo.NewGoToASTConverter()
	node, _ := converter.ConvertSourceTolerant(uri, code)
	if prog, ok := node.(*ast.ProgramStmt); ok && prog.Package != "" {
		return prog.Package
	}
	return "main"
}

func packageKeyForURI(uri, pkgName string) string {
	if parsed, err := url.Parse(uri); err == nil {
		dir := path.Dir(parsed.Path)
		if dir == "." || dir == "/" || dir == "" {
			dir = parsed.Host
		} else if parsed.Host != "" {
			dir = parsed.Host + dir
		}
		return strings.TrimRight(dir, "/") + "::" + pkgName
	}
	lastSlash := strings.LastIndex(uri, "/")
	if lastSlash == -1 {
		return uri + "::" + pkgName
	}
	return uri[:lastSlash] + "::" + pkgName
}

func rangeForScannerError(code string, scanErr scanner.Error) Range {
	lines := strings.Split(code, "\n")
	if len(lines) == 0 {
		return Range{}
	}

	lineIndex := scanErr.Pos.Line - 1
	if lineIndex < 0 {
		lineIndex = 0
	}
	if lineIndex >= len(lines) {
		lineIndex = len(lines) - 1
	}
	if strings.Contains(scanErr.Msg, "EOF") {
		for lineIndex > 0 && len([]rune(lines[lineIndex])) == 0 {
			lineIndex--
		}
	}

	lineText := lines[lineIndex]
	lineLen := len([]rune(lineText))
	startChar := scanErr.Pos.Column - 1
	if startChar < 0 {
		startChar = 0
	}
	if startChar > lineLen {
		startChar = lineLen
	}

	width := scannerErrorWidth(scanErr.Msg)
	endChar := startChar + width
	if endChar <= startChar {
		endChar = startChar + 1
	}
	if endChar > lineLen {
		endChar = lineLen
	}
	if endChar <= startChar {
		if lineLen > 0 && startChar == lineLen {
			startChar--
		}
		endChar = startChar + 1
		if endChar > lineLen {
			endChar = lineLen
		}
	}
	return Range{
		Start: Position{Line: lineIndex, Character: startChar},
		End:   Position{Line: lineIndex, Character: endChar},
	}
}

func scannerErrorWidth(message string) int {
	matches := scannerFoundTokenPattern.FindStringSubmatch(message)
	if len(matches) < 2 {
		return 1
	}
	token := strings.TrimSpace(matches[1])
	token = strings.Trim(token, "'")
	if token == "" || token == "EOF" {
		return 1
	}
	return len([]rune(token))
}

func (s *LSPServer) GetCompletions(uri string, line, char int) []CompletionItem {
	pkg := s.packageForURI(uri)
	if pkg == nil {
		return nil
	}
	pkg.mu.RLock()
	combined := pkg.combined
	pkg.mu.RUnlock()
	if combined == nil {
		return nil
	}
	items := combined.GetCompletionsAt(line+1, char+1)
	res := make([]CompletionItem, 0, len(items))
	for _, it := range items {
		res = append(res, CompletionItem{
			Label:         it.Label,
			Kind:          MapKind(it.Kind),
			Detail:        string(it.Type),
			InsertText:    it.Label,
			Documentation: it.Doc,
		})
	}
	return res
}

func (s *LSPServer) GetHover(uri string, line, char int) *Hover {
	pkg := s.packageForURI(uri)
	if pkg == nil {
		return nil
	}
	pkg.mu.RLock()
	combined := pkg.combined
	pkg.mu.RUnlock()
	if combined == nil {
		return nil
	}
	info := combined.GetHoverAt(line+1, char+1)
	if info == nil {
		return nil
	}
	return &Hover{
		Contents: MarkupContent{
			Kind:  "markdown",
			Value: fmt.Sprintf("```go\n%s\n```\n%s", info.Signature, info.Doc),
		},
	}
}

func (s *LSPServer) GetDefinition(uri string, line, char int) []Location {
	pkg := s.packageForURI(uri)
	if pkg == nil {
		return nil
	}
	pkg.mu.RLock()
	combined := pkg.combined
	pkg.mu.RUnlock()
	if combined == nil {
		return nil
	}
	def := combined.GetDefinitionAt(line+1, char+1)
	if def == nil {
		return nil
	}
	defLoc := def.GetBase().Loc
	targetURI := uri
	if defLoc != nil && defLoc.F != "" {
		targetURI = defLoc.F
	}
	return []Location{{URI: targetURI, Range: FromInternalPos(defLoc)}}
}

func (s *LSPServer) GetReferences(uri string, line, char int) []Location {
	pkg := s.packageForURI(uri)
	if pkg == nil {
		return nil
	}
	pkg.mu.RLock()
	combined := pkg.combined
	pkg.mu.RUnlock()
	if combined == nil {
		return nil
	}
	refs := combined.GetReferencesAt(line+1, char+1)
	res := make([]Location, 0, len(refs))
	for _, r := range refs {
		loc := r.GetBase().Loc
		if loc == nil {
			continue
		}
		targetURI := uri
		if loc.F != "" {
			targetURI = loc.F
		}
		res = append(res, Location{URI: targetURI, Range: FromInternalPos(loc)})
	}
	return res
}
