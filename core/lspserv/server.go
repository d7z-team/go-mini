package lspserv

import (
	"errors"
	"fmt"
	"go/scanner"
	"sync"

	engine "gopkg.d7z.net/go-mini/core"
	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

// LSPServer 是嵌入式 LSP 服务核心
type LSPServer struct {
	executor *engine.MiniExecutor
	sessions sync.Map // map[string]*fileSession (URI -> Session)
	packages sync.Map // map[string]*packageState (PkgName -> State)
}

type fileSession struct {
	pkgName string
	code    string
}

type packageState struct {
	combined *engine.MiniProgram
	files    map[string]string // URI -> Code
	mu       sync.Mutex
}

// NewLSPServer 创建一个新的 LSP 服务实例
func NewLSPServer(e *engine.MiniExecutor) *LSPServer {
	return &LSPServer{
		executor: e,
	}
}

// UpdateSession 更新或创建脚本会话并返回诊断信息
func (s *LSPServer) UpdateSession(uri, code string) ([]Diagnostic, error) {
	// 1. 尝试获取包名（简单正则或初步解析）
	pkgName := "main"
	converter := ffigo.NewGoToASTConverter()
	node, _ := converter.ConvertSourceTolerant(uri, code)
	if prog, ok := node.(*ast.ProgramStmt); ok && prog.Package != "" {
		pkgName = prog.Package
	}

	// 2. 存储文件会话
	s.sessions.Store(uri, &fileSession{pkgName: pkgName, code: code})

	// 3. 触发包级重新聚合分析
	return s.rebuildPackage(pkgName, uri)
}

func (s *LSPServer) rebuildPackage(pkgName, targetURI string) ([]Diagnostic, error) {
	val, _ := s.packages.LoadOrStore(pkgName, &packageState{files: make(map[string]string)})
	pkg := val.(*packageState)
	pkg.mu.Lock()
	defer pkg.mu.Unlock()

	// 收集该包下所有已知文件的最新代码
	s.sessions.Range(func(key, value interface{}) bool {
		sess := value.(*fileSession)
		if sess.pkgName == pkgName {
			pkg.files[key.(string)] = sess.code
		}
		return true
	})

	// 聚合所有文件到单个 ProgramStmt
	var combinedNode *ast.ProgramStmt
	converter := ffigo.NewGoToASTConverter()
	diagnostics := make([]Diagnostic, 0)

	for uri, code := range pkg.files {
		node, errs := converter.ConvertSourceTolerant(uri, code)

		// 语法错误单独处理
		for _, err := range errs {
			var scanErr scanner.Error
			if errors.As(err, &scanErr) {
				if scanErr.Pos.Filename == targetURI {
					diagnostics = append(diagnostics, Diagnostic{
						Range: Range{
							Start: Position{Line: scanErr.Pos.Line - 1, Character: scanErr.Pos.Column - 1},
							End:   Position{Line: scanErr.Pos.Line - 1, Character: scanErr.Pos.Column},
						},
						Severity: 1,
						Source:   "go-mini-syntax",
						Message:  scanErr.Msg,
					})
				}
			} else if err != nil && uri == targetURI {
				diagnostics = append(diagnostics, Diagnostic{
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
				// 合并符号（借用我们在 cmd/exec 中定义的逻辑思路）
				mergeProgramStmts(combinedNode, prog)
			}
		}
	}

	if combinedNode == nil {
		return diagnostics, nil
	}

	// 运行校验以生成持久化 Scope
	prog, errs := s.executor.NewMiniProgramByProgramTolerant(combinedNode)
	pkg.combined = prog

	// 将当前触发文件的诊断信息返回
	for _, err := range errs {
		if astErr, ok := err.(*ast.MiniAstError); ok {
			for _, log := range astErr.Logs {
				loc := log.Node.GetBase().Loc
				// 严格过滤文件路径，确保错误显示在正确的文件中
				if loc != nil && loc.F == targetURI {
					diag := Diagnostic{
						Range:    FromInternalPos(loc),
						Severity: 1,
						Source:   "go-mini",
						Message:  log.Message,
					}
					diagnostics = append(diagnostics, diag)
				}
			}
		} else {
			// 处理其他类型的运行时或执行错误
			var vme *runtime.VMError
			if errors.As(err, &vme) {
				if len(vme.Frames) > 0 && vme.Frames[0].Filename == targetURI {
					diag := Diagnostic{
						Range: FromInternalPos(&ast.Position{
							L: vme.Frames[0].Line,
							C: vme.Frames[0].Column,
						}),
						Severity: 1,
						Source:   "go-mini-runtime",
						Message:  vme.Message,
					}
					for _, f := range vme.Frames {
						diag.RelatedInformation = append(diag.RelatedInformation, DiagnosticRelatedInformation{
							Location: Location{
								URI: f.Filename,
								Range: FromInternalPos(&ast.Position{
									L: f.Line, C: f.Column,
									EL: f.Line, EC: f.Column + 1,
								}),
							},
							Message: fmt.Sprintf("at %s()", f.Function),
						})
					}
					diagnostics = append(diagnostics, diag)
				}
			}
		}
	}

	return diagnostics, nil
}

func mergeProgramStmts(dest, src *ast.ProgramStmt) {
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
	for k, v := range src.Interfaces {
		dest.Interfaces[k] = v
	}
	dest.Main = append(dest.Main, src.Main...)
}

// GetCompletions 获取指定位置的补全建议
func (s *LSPServer) GetCompletions(uri string, line, char int) []CompletionItem {
	val, ok := s.sessions.Load(uri)
	if !ok {
		return nil
	}
	sess := val.(*fileSession)

	pVal, ok := s.packages.Load(sess.pkgName)
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

	pVal, ok := s.packages.Load(sess.pkgName)
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

	pVal, ok := s.packages.Load(sess.pkgName)
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

	pVal, ok := s.packages.Load(sess.pkgName)
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
