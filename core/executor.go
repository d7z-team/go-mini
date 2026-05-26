package engine

import (
	"sync"

	"gopkg.d7z.net/go-mini/core/ast"
	"gopkg.d7z.net/go-mini/core/calltemplate"
	"gopkg.d7z.net/go-mini/core/compiler"
	"gopkg.d7z.net/go-mini/core/ffigo"
	"gopkg.d7z.net/go-mini/core/runtime"
	"gopkg.d7z.net/go-mini/core/surface"
)

type MiniExecutor struct {
	mu sync.RWMutex

	routes    map[string]runtime.FFIRoute
	constants map[string]string

	registry            *ffigo.HandleRegistry
	moduleSources       map[string]*ast.ProgramStmt
	sourceLibraries     map[string]surface.LibraryModule
	modules             map[string]*runtime.PreparedProgram
	librarySourceHashes map[string]string
	libraryHashes       map[string]string
	funcSchemas         map[ast.Ident]*runtime.RuntimeFuncSig
	valueSchemas        map[ast.Ident]*runtime.ValueSpec
	packageValues       map[string]*runtime.BoundPackageValue
	surfaceSchema       *runtime.FFISurfaceSchema
	boundSurface        *runtime.BoundFFISurface
	structsMeta         map[ast.Ident]*runtime.RuntimeStructSpec
	interfacesMeta      map[ast.Ident]*runtime.RuntimeInterfaceSpec
	templates           *calltemplate.Registry

	MaxTypeDepth int // 递归类型检查深度限制
}

type SourceFile = compiler.SourceFile

func SourceFileExt() string {
	return compiler.ScriptFileExt
}

type ExportedSchemaSnapshot struct {
	Funcs                   map[ast.Ident]*runtime.RuntimeFuncSig
	RegisteredFuncs         map[ast.Ident]bool
	RegisteredFuncMethodIDs map[ast.Ident]uint32
	Values                  map[ast.Ident]*runtime.ValueSpec
	Structs                 map[ast.Ident]*runtime.RuntimeStructSpec
	Interfaces              map[ast.Ident]*runtime.RuntimeInterfaceSpec
}

func (e *MiniExecutor) SetMaxTypeDepth(depth int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.MaxTypeDepth = depth
}
