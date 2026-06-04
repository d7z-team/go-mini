package lspserv

import (
	"go/scanner"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/gofrontend"
)

type ProgramView interface {
	GetCompletionsAtFile(file string, line, col int) []ast.CompletionItem
	GetHoverAtFile(file string, line, col int) *ast.HoverInfo
	GetDefinitionAtFile(file string, line, col int) ast.Node
	GetReferencesAtFile(file string, line, col int, includeDeclaration bool) []ast.Node
}

type signatureHelpProgramView interface {
	GetSignatureHelpAtFile(file string, line, col int) *ast.SignatureHelpInfo
}

type documentSymbolProgramView interface {
	GetDocumentSymbolsAtFile(file string) []ast.DocumentSymbolInfo
}

type importPathResolverProgramView interface {
	ResolveImportPathForPackage(alias string) string
}

type SnapshotFile struct {
	URI     string
	Path    string
	Code    string
	Open    bool
	Version uint64
}

type PackageSnapshot struct {
	Key         string
	PackageName string
	RootURI     string
	RootPath    string
	Version     uint64
	Files       []SnapshotFile
}

type AnalysisOptions struct {
	CompileParity bool
}

type AnalysisResult struct {
	Program     ProgramView
	Diagnostics map[string][]Diagnostic
}

type Analyzer interface {
	// AnalyzeSnapshot analyzes a package snapshot for LSP without depending on
	// the runtime execution path.
	AnalyzeSnapshot(snapshot PackageSnapshot, options AnalysisOptions) (AnalysisResult, error)
}

type LSPServer struct {
	executor Analyzer

	mu                 sync.RWMutex
	files              map[string]*fileSession
	packages           map[string]*packageState
	publishDiagnostics func(map[string][]Diagnostic)
	diagnosticDelay    time.Duration
}

type fileSession struct {
	uri      string
	path     string
	pkgKey   string
	pkgName  string
	code     string
	version  uint64
	modified uint64
}

type packageState struct {
	key      string
	pkgName  string
	rootURI  string
	rootPath string

	mu sync.RWMutex

	files                map[string]*fileSession
	combined             ProgramView
	analysisVersion      uint64
	publishedDiagnostics map[string][]Diagnostic
	diagnosticTimer      *time.Timer
	diagnosticGeneration uint64
	version              uint64
}

type packageAnalysis struct {
	version     uint64
	combined    ProgramView
	diagnostics map[string][]Diagnostic
}

var scannerFoundTokenPattern = regexp.MustCompile(`found\s+('?[^']+'?|\S+)$`)

const defaultDiagnosticDelay = 180 * time.Millisecond

func NewLSPServer(e Analyzer) *LSPServer {
	return &LSPServer{
		executor:        e,
		files:           make(map[string]*fileSession),
		packages:        make(map[string]*packageState),
		diagnosticDelay: defaultDiagnosticDelay,
	}
}

func (s *LSPServer) setDiagnosticPublisher(publish func(map[string][]Diagnostic)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishDiagnostics = publish
}

func (s *LSPServer) UpdateSession(uri, code string) (map[string][]Diagnostic, error) {
	oldPkg, currentPkg := s.applySession(uri, code)
	result := make(map[string][]Diagnostic)
	if oldPkg != nil {
		oldDiagnostics, _ := s.flushPackageDiagnostics(oldPkg)
		mergeDiagnostics(result, oldDiagnostics)
	}
	currentDiagnostics, err := s.flushPackageDiagnostics(currentPkg)
	mergeDiagnostics(result, currentDiagnostics)
	return result, err
}

func (s *LSPServer) updateSessionDebounced(uri, code string) {
	oldPkg, currentPkg := s.applySession(uri, code)
	if oldPkg != nil {
		s.schedulePackageDiagnostics(oldPkg)
	}
	s.schedulePackageDiagnostics(currentPkg)
}

func (s *LSPServer) flushDiagnostics(uri string) (map[string][]Diagnostic, error) {
	pkg := s.packageForURI(uri)
	if pkg == nil {
		return nil, nil
	}
	return s.flushPackageDiagnostics(pkg)
}

func (s *LSPServer) applySession(uri, code string) (*packageState, *packageState) {
	pkgName := detectPackageName(uri, code)
	rootURI, rootPath := packageRootForURI(uri)
	pkgKey := packageKeyForURI(uri, pkgName)

	s.mu.Lock()
	file := s.files[uri]
	if file == nil {
		file = &fileSession{uri: uri, path: localPathForURI(uri)}
		s.files[uri] = file
	}
	oldPkgKey := file.pkgKey
	file.version++
	file.modified = file.version
	file.pkgKey = pkgKey
	file.pkgName = pkgName
	file.code = code

	currentPkg := s.ensurePackageLocked(pkgKey, pkgName, rootURI, rootPath)
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
			shouldDelete := len(oldPkg.files) == 0 && !packageHasDiskFiles(oldPkg)
			oldPkg.mu.Unlock()
			if shouldDelete {
				delete(s.packages, oldPkgKey)
			}
		}
	}
	s.mu.Unlock()
	return oldPkg, currentPkg
}

func (s *LSPServer) RemoveSession(uri string) map[string][]Diagnostic {
	s.mu.Lock()
	file := s.files[uri]
	if file == nil {
		s.mu.Unlock()
		return nil
	}
	delete(s.files, uri)
	pkg := s.packages[file.pkgKey]
	if pkg == nil {
		s.mu.Unlock()
		return map[string][]Diagnostic{uri: make([]Diagnostic, 0)}
	}
	s.cancelPackageDiagnostics(pkg)

	pkg.mu.Lock()
	delete(pkg.files, uri)
	pkg.version++
	shouldDelete := len(pkg.files) == 0 && !packageHasDiskFiles(pkg)
	if shouldDelete {
		updates := make(map[string][]Diagnostic)
		for diagURI, old := range pkg.publishedDiagnostics {
			if len(old) > 0 {
				updates[diagURI] = []Diagnostic{}
			}
		}
		if _, ok := updates[uri]; !ok {
			updates[uri] = []Diagnostic{}
		}
		delete(s.packages, file.pkgKey)
		pkg.mu.Unlock()
		s.mu.Unlock()
		return updates
	}
	pkg.mu.Unlock()
	s.mu.Unlock()

	updates, _ := s.flushPackageDiagnostics(pkg)
	if _, ok := updates[uri]; !ok && !packageContainsDiskURI(pkg, uri) {
		updates[uri] = []Diagnostic{}
	}
	return updates
}

func (s *LSPServer) RefreshWorkspaceFiles(uris []string) (map[string][]Diagnostic, error) {
	if len(uris) == 0 {
		return nil, nil
	}
	roots := make(map[string]struct{}, len(uris))
	for _, uri := range uris {
		if local := localPathForURI(uri); local != "" {
			roots[filepath.Dir(local)] = struct{}{}
		}
	}

	s.mu.RLock()
	packages := make([]*packageState, 0)
	seen := make(map[string]struct{})
	for _, pkg := range s.packages {
		if pkg == nil {
			continue
		}
		if _, ok := roots[pkg.rootPath]; !ok {
			continue
		}
		if _, duplicate := seen[pkg.key]; duplicate {
			continue
		}
		seen[pkg.key] = struct{}{}
		packages = append(packages, pkg)
	}
	s.mu.RUnlock()

	updates := make(map[string][]Diagnostic)
	var firstErr error
	for _, pkg := range packages {
		pkg.mu.Lock()
		pkg.version++
		pkg.mu.Unlock()
		current, err := s.flushPackageDiagnostics(pkg)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		mergeDiagnostics(updates, current)
	}
	return updates, firstErr
}

func (s *LSPServer) stopPendingDiagnostics() {
	s.mu.RLock()
	packages := make([]*packageState, 0, len(s.packages))
	for _, pkg := range s.packages {
		packages = append(packages, pkg)
	}
	s.mu.RUnlock()
	for _, pkg := range packages {
		s.cancelPackageDiagnostics(pkg)
	}
}

func (s *LSPServer) schedulePackageDiagnostics(pkg *packageState) {
	if pkg == nil {
		return
	}
	delay := s.diagnosticDelay
	if delay <= 0 {
		updates, _ := s.flushPackageDiagnostics(pkg)
		s.publishDiagnosticUpdates(updates)
		return
	}

	pkg.mu.Lock()
	if pkg.diagnosticTimer != nil {
		pkg.diagnosticTimer.Stop()
	}
	pkg.diagnosticGeneration++
	generation := pkg.diagnosticGeneration
	pkg.diagnosticTimer = time.AfterFunc(delay, func() {
		if !clearScheduledPackageTimer(pkg, generation) {
			return
		}
		updates, _ := s.flushPackageDiagnostics(pkg)
		s.publishDiagnosticUpdates(updates)
	})
	pkg.mu.Unlock()
}

func clearScheduledPackageTimer(pkg *packageState, generation uint64) bool {
	pkg.mu.Lock()
	defer pkg.mu.Unlock()
	if pkg.diagnosticGeneration != generation || pkg.diagnosticTimer == nil {
		return false
	}
	pkg.diagnosticTimer = nil
	return true
}

func (s *LSPServer) cancelPackageDiagnostics(pkg *packageState) {
	if pkg == nil {
		return
	}
	pkg.mu.Lock()
	defer pkg.mu.Unlock()
	s.cancelPackageDiagnosticsLocked(pkg)
}

func (s *LSPServer) cancelPackageDiagnosticsLocked(pkg *packageState) {
	if pkg.diagnosticTimer != nil {
		pkg.diagnosticTimer.Stop()
		pkg.diagnosticTimer = nil
	}
}

func (s *LSPServer) publishDiagnosticUpdates(updates map[string][]Diagnostic) {
	if len(updates) == 0 {
		return
	}
	s.mu.RLock()
	publish := s.publishDiagnostics
	s.mu.RUnlock()
	if publish != nil {
		publish(updates)
	}
}

func (s *LSPServer) ensurePackageLocked(pkgKey, pkgName, rootURI, rootPath string) *packageState {
	if pkg := s.packages[pkgKey]; pkg != nil {
		pkg.pkgName = pkgName
		pkg.rootURI = rootURI
		pkg.rootPath = rootPath
		return pkg
	}
	pkg := &packageState{
		key:                  pkgKey,
		pkgName:              pkgName,
		rootURI:              rootURI,
		rootPath:             rootPath,
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

func (s *LSPServer) analyzePackage(pkg *packageState) (*packageAnalysis, error) {
	if pkg == nil {
		return nil, nil
	}

	snapshot := s.snapshotPackage(pkg)
	if len(snapshot.Files) == 0 {
		return &packageAnalysis{version: snapshot.Version, diagnostics: nil}, nil
	}
	result, err := s.executor.AnalyzeSnapshot(snapshot, AnalysisOptions{CompileParity: true})
	return &packageAnalysis{version: snapshot.Version, combined: result.Program, diagnostics: result.Diagnostics}, err
}

func (s *LSPServer) flushPackageDiagnostics(pkg *packageState) (map[string][]Diagnostic, error) {
	s.cancelPackageDiagnostics(pkg)
	analysis, err := s.analyzePackage(pkg)
	return applyPackageAnalysis(pkg, analysis, true), err
}

func (s *LSPServer) refreshPackageAnalysis(pkg *packageState) ProgramView {
	if pkg == nil {
		return nil
	}
	pkg.mu.RLock()
	if pkg.analysisVersion == pkg.version {
		combined := pkg.combined
		pkg.mu.RUnlock()
		return combined
	}
	pkg.mu.RUnlock()

	analysis, _ := s.analyzePackage(pkg)
	applyPackageAnalysis(pkg, analysis, false)

	pkg.mu.RLock()
	combined := pkg.combined
	pkg.mu.RUnlock()
	return combined
}

func (s *LSPServer) snapshotPackage(pkg *packageState) PackageSnapshot {
	pkg.mu.RLock()
	key := pkg.key
	pkgName := pkg.pkgName
	rootURI := pkg.rootURI
	rootPath := pkg.rootPath
	version := pkg.version
	openFiles := make([]*fileSession, 0, len(pkg.files))
	for _, file := range pkg.files {
		openFiles = append(openFiles, cloneFileSession(file))
	}
	pkg.mu.RUnlock()

	byURI := make(map[string]SnapshotFile)
	for _, file := range diskPackageFiles(rootPath, pkgName) {
		byURI[file.URI] = file
	}
	for _, file := range openFiles {
		if file == nil {
			continue
		}
		byURI[file.uri] = SnapshotFile{
			URI:     file.uri,
			Path:    file.path,
			Code:    file.code,
			Open:    true,
			Version: file.version,
		}
	}

	files := make([]SnapshotFile, 0, len(byURI))
	for _, file := range byURI {
		files = append(files, file)
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].URI < files[j].URI
	})

	return PackageSnapshot{
		Key:         key,
		PackageName: pkgName,
		RootURI:     rootURI,
		RootPath:    rootPath,
		Version:     version,
		Files:       files,
	}
}

func applyPackageAnalysis(pkg *packageState, analysis *packageAnalysis, publish bool) map[string][]Diagnostic {
	if pkg == nil || analysis == nil {
		return nil
	}
	pkg.mu.Lock()
	defer pkg.mu.Unlock()
	if analysis.version != pkg.version {
		return nil
	}
	pkg.combined = analysis.combined
	pkg.analysisVersion = analysis.version
	if !publish {
		return nil
	}
	return diffDiagnosticsLocked(pkg, analysis.diagnostics)
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
		if a[i].Range != b[i].Range || a[i].Severity != b[i].Severity || a[i].Code != b[i].Code || a[i].Source != b[i].Source || a[i].Message != b[i].Message {
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
	if file != nil {
		return s.packages[file.pkgKey]
	}
	if localPath := localPathForURI(uri); localPath != "" {
		data, err := os.ReadFile(localPath)
		if err == nil {
			pkgKey := packageKeyForURI(uri, detectPackageName(uri, string(data)))
			return s.packages[pkgKey]
		}
	}
	return nil
}

func (s *LSPServer) programForURI(uri string) (*packageState, ProgramView) {
	pkg := s.packageForURI(uri)
	if pkg == nil {
		return nil, nil
	}
	return pkg, s.refreshPackageAnalysis(pkg)
}

func mergeDiagnostics(dst, src map[string][]Diagnostic) {
	for uri, diags := range src {
		dst[uri] = diags
	}
}

func detectPackageName(uri, code string) string {
	converter := gofrontend.NewConverter()
	node, _ := converter.ConvertSourceTolerant(uri, code)
	if prog, ok := node.(*ast.ProgramStmt); ok && prog.Package != "" {
		return prog.Package
	}
	return "main"
}

func packageKeyForURI(uri, pkgName string) string {
	rootURI, _ := packageRootForURI(uri)
	if rootURI != "" {
		return rootURI + "::" + pkgName
	}
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

func packageRootForURI(uri string) (string, string) {
	if localPath := localPathForURI(uri); localPath != "" {
		rootPath := filepath.Dir(localPath)
		return fileURIForPath(rootPath), rootPath
	}
	if parsed, err := url.Parse(uri); err == nil {
		dir := path.Dir(parsed.Path)
		if dir == "." || dir == "/" || dir == "" {
			dir = parsed.Host
		} else if parsed.Host != "" {
			dir = parsed.Host + dir
		}
		return strings.TrimRight(dir, "/"), ""
	}
	lastSlash := strings.LastIndex(uri, "/")
	if lastSlash == -1 {
		return uri, ""
	}
	return uri[:lastSlash], ""
}

func localPathForURI(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil || parsed.Scheme != "file" {
		return ""
	}
	pathText, err := url.PathUnescape(parsed.Path)
	if err != nil {
		pathText = parsed.Path
	}
	if pathText == "" {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(pathText))
}

func fileURIForPath(name string) string {
	if name == "" {
		return ""
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		abs = name
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}).String()
}

func packageHasDiskFiles(pkg *packageState) bool {
	if pkg == nil || pkg.rootPath == "" {
		return false
	}
	return len(diskPackageFiles(pkg.rootPath, pkg.pkgName)) > 0
}

func packageContainsDiskURI(pkg *packageState, uri string) bool {
	if pkg == nil || pkg.rootPath == "" || uri == "" {
		return false
	}
	local := localPathForURI(uri)
	if local == "" || filepath.Dir(local) != pkg.rootPath {
		return false
	}
	data, err := os.ReadFile(local)
	if err != nil {
		return false
	}
	return detectPackageName(uri, string(data)) == pkg.pkgName
}

func diskPackageFiles(rootPath, pkgName string) []SnapshotFile {
	if rootPath == "" {
		return nil
	}
	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil
	}
	files := make([]SnapshotFile, 0)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".mgo" {
			continue
		}
		name := filepath.Join(rootPath, entry.Name())
		data, err := os.ReadFile(name)
		if err != nil {
			continue
		}
		uri := fileURIForPath(name)
		code := string(data)
		if detectPackageName(uri, code) != pkgName {
			continue
		}
		files = append(files, SnapshotFile{URI: uri, Path: name, Code: code})
	}
	return files
}

func RangeForScannerError(code string, scanErr scanner.Error) Range {
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
	lineLenBytes := len(lineText)
	lineLenUTF16 := utf16CharacterForByteColumn(lineText, lineLenBytes+1)
	startByte := scanErr.Pos.Column - 1
	if startByte < 0 {
		startByte = 0
	}
	if startByte > lineLenBytes {
		startByte = lineLenBytes
	}

	token := scannerErrorToken(scanErr.Msg)
	endByte := startByte + len(token)
	if token == "" {
		endByte = startByte + 1
	}
	if endByte > lineLenBytes {
		endByte = lineLenBytes
	}

	startChar := utf16CharacterForByteColumn(lineText, startByte+1)
	endChar := utf16CharacterForByteColumn(lineText, endByte+1)
	if endChar <= startChar {
		if lineLenUTF16 > 0 && startChar == lineLenUTF16 {
			startChar--
		}
		endChar = startChar + 1
		if endChar > lineLenUTF16 {
			endChar = lineLenUTF16
		}
	}
	return Range{
		Start: Position{Line: lineIndex, Character: startChar},
		End:   Position{Line: lineIndex, Character: endChar},
	}
}

func scannerErrorToken(message string) string {
	matches := scannerFoundTokenPattern.FindStringSubmatch(message)
	if len(matches) < 2 {
		return ""
	}
	token := strings.TrimSpace(matches[1])
	token = strings.Trim(token, "'")
	if token == "" || token == "EOF" {
		return ""
	}
	return token
}
