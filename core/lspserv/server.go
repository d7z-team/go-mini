package lspserv

import (
	"errors"
	"fmt"
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
	return s.rebuildPackage(pkgName)
}

func (s *LSPServer) rebuildPackage(pkgName string) ([]Diagnostic, error) {
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

	for uri, code := range pkg.files {
		node, _ := converter.ConvertSourceTolerant(uri, code)

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
		return nil, nil
	}

	// 运行校验以生成持久化 Scope
	prog, errs := s.executor.NewMiniProgramByAstTolerant(combinedNode)
	pkg.combined = prog

	// 将当前触发文件的诊断信息返回
	diagnostics := make([]Diagnostic, 0)
	for _, err := range errs {
		if astErr, ok := err.(*ast.MiniAstError); ok {
			for _, log := range astErr.Logs {
				loc := log.Node.GetBase().Loc
				diag := Diagnostic{
					Range:    FromInternalPos(loc),
					Severity: 1,
					Source:   "go-mini",
					Message:  log.Message,
				}
				diagnostics = append(diagnostics, diag)
			}
		} else {
			// 处理其他类型的运行时或执行错误
			var vme *runtime.VMError
			if errors.As(err, &vme) {
				diag := Diagnostic{
					Range:    FromInternalPos(&ast.Position{L: 1, C: 1}), // 默认位置
					Severity: 1,
					Source:   "go-mini-runtime",
					Message:  vme.Message,
				}
				if len(vme.Frames) > 0 {
					diag.Range = FromInternalPos(&ast.Position{
						L: vme.Frames[0].Line,
						C: vme.Frames[0].Column,
					})
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
				}
				diagnostics = append(diagnostics, diag)
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
			Label:      it.Label,
			Kind:       MapKind(it.Kind),
			Detail:     string(it.Type),
			InsertText: it.Label,
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

	return []Location{
		{
			URI:   uri, // 目前假设定义在同一个 URI，实际应从 def 的 Loc 溯源
			Range: FromInternalPos(def.GetBase().Loc),
		},
	}
}
