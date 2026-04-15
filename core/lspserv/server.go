package lspserv

import (
	"errors"
	"fmt"
	"go/scanner"
	"net/url"
	"path"
	"strings"
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
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

// LSPServer 是嵌入式 LSP 服务核心
type LSPServer struct {
	executor Analyzer
	sessions sync.Map // map[string]*fileSession (URI -> Session)
	packages sync.Map // map[string]*packageState (PkgName -> State)
}

type fileSession struct {
	pkgKey string
	code   string
}

type packageState struct {
	combined ProgramView
	files    map[string]string // URI -> Code
	mu       sync.Mutex
}

// NewLSPServer 创建一个新的 LSP 服务实例
func NewLSPServer(e Analyzer) *LSPServer {
	return &LSPServer{
		executor: e,
	}
}

// UpdateSession 更新或创建脚本会话并返回诊断信息。
func (s *LSPServer) UpdateSession(uri, code string) (map[string][]Diagnostic, error) {
	pkgName := "main"
	converter := ffigo.NewGoToASTConverter()
	node, _ := converter.ConvertSourceTolerant(uri, code)
	if prog, ok := node.(*ast.ProgramStmt); ok && prog.Package != "" {
		pkgName = prog.Package
	}

	pkgKey := packageKeyForURI(uri, pkgName)
	s.sessions.Store(uri, &fileSession{pkgKey: pkgKey, code: code})

	return s.rebuildPackage(pkgKey)
}

func (s *LSPServer) rebuildPackage(pkgKey string) (map[string][]Diagnostic, error) {
	val, _ := s.packages.LoadOrStore(pkgKey, &packageState{files: make(map[string]string)})
	pkg := val.(*packageState)
	pkg.mu.Lock()
	defer pkg.mu.Unlock()
	pkg.files = make(map[string]string)

	// 收集该包下所有已知文件的最新代码
	s.sessions.Range(func(key, value interface{}) bool {
		sess := value.(*fileSession)
		if sess.pkgKey == pkgKey {
			pkg.files[key.(string)] = sess.code
		}
		return true
	})

	var combinedNode *ast.ProgramStmt
	converter := ffigo.NewGoToASTConverter()
	allDiagnostics := make(map[string][]Diagnostic)

	for uri, code := range pkg.files {
		node, errs := converter.ConvertSourceTolerant(uri, code)

		// 语法错误单独处理
		for _, err := range errs {
			var scanErr scanner.Error
			if errors.As(err, &scanErr) {
				allDiagnostics[uri] = append(allDiagnostics[uri], Diagnostic{
					Range: Range{
						Start: Position{Line: scanErr.Pos.Line - 1, Character: scanErr.Pos.Column - 1},
						End:   Position{Line: scanErr.Pos.Line - 1, Character: scanErr.Pos.Column},
					},
					Severity: 1,
					Source:   "go-mini-syntax",
					Message:  scanErr.Msg,
				})
			} else if err != nil {
				allDiagnostics[uri] = append(allDiagnostics[uri], Diagnostic{
					Range:    FromInternalPos(&ast.Position{L: 1, C: 1}),
					Severity: 1,
					Source:   "go-mini-syntax",
					Message:  err.Error(),
				})
			}
		}

		if prog, ok := node.(*ast.ProgramStmt); ok {
			if combinedNode == nil {
				combinedNode = prog
			} else {
				mergeProgramStmts(combinedNode, prog)
			}
		}
	}

	if combinedNode == nil {
		return allDiagnostics, nil
	}

	// 运行校验以生成持久化 Scope
	prog, errs := s.executor.AnalyzeProgramTolerant(combinedNode)
	pkg.combined = prog

	// 收集所有文件的诊断信息
	for _, err := range errs {
		if astErr, ok := err.(*ast.MiniAstError); ok {
			for _, log := range astErr.Logs {
				loc := log.Node.GetBase().Loc
				if loc != nil && loc.F != "" {
					diag := Diagnostic{
						Range:    FromInternalPos(loc),
						Severity: 1,
						Source:   "go-mini",
						Message:  log.Message,
					}
					allDiagnostics[loc.F] = append(allDiagnostics[loc.F], diag)
				}
			}
		} else {
			// 处理运行时错误
			var vme *runtime.VMError
			if errors.As(err, &vme) {
				if len(vme.Frames) > 0 {
					f := vme.Frames[0]
					diag := Diagnostic{
						Range: FromInternalPos(&ast.Position{
							L: f.Line,
							C: f.Column,
						}),
						Severity: 1,
						Source:   "go-mini-runtime",
						Message:  vme.Message,
					}
					allDiagnostics[f.Filename] = append(allDiagnostics[f.Filename], diag)
				}
			}
		}
	}

	return allDiagnostics, nil
}

func mergeProgramStmts(dest, src *ast.ProgramStmt) {
	if len(dest.Imports) == 0 {
		dest.Imports = append(dest.Imports, src.Imports...)
	} else {
		seen := make(map[string]struct{}, len(dest.Imports))
		for _, imp := range dest.Imports {
			seen[imp.Alias+"|"+imp.Path] = struct{}{}
		}
		for _, imp := range src.Imports {
			key := imp.Alias + "|" + imp.Path
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			dest.Imports = append(dest.Imports, imp)
		}
	}
	for k, v := range src.Functions {
		dest.Functions[k] = v
	}
	for k, v := range src.Structs {
		dest.Structs[k] = v
	}
	for k, v := range src.Variables {
		dest.Variables[k] = v
	}
	for k, v := range src.Constants {
		dest.Constants[k] = v
	}
	for k, v := range src.Types {
		dest.Types[k] = v
	}
	for k, v := range src.Interfaces {
		dest.Interfaces[k] = v
	}
	dest.Main = append(dest.Main, src.Main...)
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

// GetCompletions 获取指定位置的补全建议
func (s *LSPServer) GetCompletions(uri string, line, char int) []CompletionItem {
	val, ok := s.sessions.Load(uri)
	if !ok {
		return nil
	}
	sess := val.(*fileSession)

	pVal, ok := s.packages.Load(sess.pkgKey)
	if !ok {
		return nil
	}
	pkg := pVal.(*packageState)

	if pkg.combined == nil {
		return nil
	}

	items := pkg.combined.GetCompletionsAt(line+1, char+1)

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

// GetHover 获取指定位置的悬浮信息
func (s *LSPServer) GetHover(uri string, line, char int) *Hover {
	val, ok := s.sessions.Load(uri)
	if !ok {
		return nil
	}
	sess := val.(*fileSession)

	pVal, ok := s.packages.Load(sess.pkgKey)
	if !ok {
		return nil
	}
	pkg := pVal.(*packageState)

	if pkg.combined == nil {
		return nil
	}

	info := pkg.combined.GetHoverAt(line+1, char+1)
	if info == nil {
		return nil
	}

	return &Hover{
		Contents: MarkupContent{
			Kind:  "markdown",
			Value: fmt.Sprintf("```go\n%s\n```\n%s", info.Signature, info.Doc),
		},
		Range: nil,
	}
}

// GetDefinition 获取指定位置的定义
func (s *LSPServer) GetDefinition(uri string, line, char int) []Location {
	val, ok := s.sessions.Load(uri)
	if !ok {
		return nil
	}
	sess := val.(*fileSession)

	pVal, ok := s.packages.Load(sess.pkgKey)
	if !ok {
		return nil
	}
	pkg := pVal.(*packageState)

	if pkg.combined == nil {
		return nil
	}

	def := pkg.combined.GetDefinitionAt(line+1, char+1)
	if def == nil {
		return nil
	}

	defLoc := def.GetBase().Loc
	targetURI := uri
	if defLoc != nil && defLoc.F != "" {
		targetURI = defLoc.F
	}

	return []Location{
		{
			URI:   targetURI,
			Range: FromInternalPos(defLoc),
		},
	}
}

// GetReferences 获取指定位置符号的所有引用
func (s *LSPServer) GetReferences(uri string, line, char int) []Location {
	val, ok := s.sessions.Load(uri)
	if !ok {
		return nil
	}
	sess := val.(*fileSession)

	pVal, ok := s.packages.Load(sess.pkgKey)
	if !ok {
		return nil
	}
	pkg := pVal.(*packageState)

	if pkg.combined == nil {
		return nil
	}

	refs := pkg.combined.GetReferencesAt(line+1, char+1)
	res := make([]Location, 0, len(refs))
	for _, r := range refs {
		loc := r.GetBase().Loc
		if loc != nil {
			targetURI := uri
			if loc.F != "" {
				targetURI = loc.F
			}
			res = append(res, Location{
				URI:   targetURI,
				Range: FromInternalPos(loc),
			})
		}
	}
	return res
}
