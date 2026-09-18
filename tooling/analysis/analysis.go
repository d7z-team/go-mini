// Package analysis provides compatibility access to compiler-owned source analysis.
package analysis

import (
	core "github.com/d7z-team/mini-go/compiler/analysis"
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/semantic"
)

type (
	SymbolKey         = core.SymbolKey
	Occurrence        = core.Occurrence
	Package           = core.Package
	PublicAPI         = core.PublicAPI
	PublicDeclaration = core.PublicDeclaration
	PublicField       = core.PublicField
	WorkspaceRequest  = core.WorkspaceRequest
	WorkspaceResult   = core.WorkspaceResult
)

func ProjectPublicAPI(source Package) PublicAPI { return core.ProjectPublicAPI(source) }
func CheckProgram(program ast.Program, options semantic.AnalyzeOptions) Package {
	return core.CheckProgram(program, options)
}
func Index(source Package) Package { return core.Index(source) }
func CheckWorkspace(request WorkspaceRequest) (WorkspaceResult, error) {
	return core.CheckWorkspace(request)
}
