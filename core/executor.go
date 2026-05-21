package engine

import (
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
)

type MiniExecutor struct {
	mu sync.RWMutex

	astModuleLoader func(path string) (*ast.ProgramStmt, error)
	routes          map[string]runtime.FFIRoute
	constants       map[string]string

	registry       *ffigo.HandleRegistry
	moduleSources  map[string]*ast.ProgramStmt
	modules        map[string]*runtime.PreparedProgram
	funcSchemas    map[ast.Ident]*runtime.RuntimeFuncSig
	structsMeta    map[ast.Ident]*runtime.RuntimeStructSpec
	interfacesMeta map[ast.Ident]*runtime.RuntimeInterfaceSpec

	MaxTypeDepth int // 递归类型检查深度限制
}

type SourceFile = compiler.SourceFile

func SourceFileExt() string {
	return compiler.ScriptFileExt
}

type ExportedSchemaSnapshot struct {
	Funcs      map[ast.Ident]*runtime.RuntimeFuncSig
	Structs    map[ast.Ident]*runtime.RuntimeStructSpec
	Interfaces map[ast.Ident]*runtime.RuntimeInterfaceSpec
}

func (e *MiniExecutor) SetMaxTypeDepth(depth int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.MaxTypeDepth = depth
}
