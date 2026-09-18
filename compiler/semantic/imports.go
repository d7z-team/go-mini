package semantic

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
)

func (a *analyzer) validateImportUsage(program ast.Program) {
	used := make(map[ast.NodeID]bool)
	for _, id := range a.info.Uses {
		object := a.info.Objects[id]
		if scope := a.info.Scope(object.Scope); scope != nil && scope.Kind == ScopeFile {
			used[object.Node] = true
		}
	}
	for _, file := range program.Files {
		scope := a.info.Scope(a.info.NodeScopes[file.NodeID])
		if scope == nil {
			continue
		}
		for _, decl := range file.Decls {
			if decl.Kind != ast.DeclImport {
				continue
			}
			if decl.Import.Path == program.ModulePath {
				a.addDiagnostic("semantic.import.self", "package cannot import itself", decl.Span)
				continue
			}
			if !decl.Import.IsEmbedMarker() {
				if _, ok := a.dependencies[decl.Import.Path]; !ok {
					a.addDiagnostic("semantic.dependency.unavailable", "dependency metadata unavailable for "+decl.Import.Path, decl.Span)
				}
			}
			name := decl.Import.Alias
			if name == "_" || decl.Import.IsEmbedMarker() {
				continue
			}
			if name == "" {
				name = decl.Import.Path
				if slash := strings.LastIndexByte(name, '/'); slash >= 0 {
					name = name[slash+1:]
				}
			}
			if name == "" {
				a.addDiagnostic("semantic.import.alias", "import requires a usable alias", decl.Span)
				continue
			}
			if name != "." {
				if _, exists := a.info.Scope(a.info.PackageScope).Objects[name]; exists {
					a.addDiagnostic("semantic.scope.file_package", "identifier cannot be declared in both file and package block", decl.Span)
					continue
				}
				if object, exists := scope.Objects[name]; exists && a.info.Objects[object].Node != decl.NodeID {
					a.addDiagnostic("semantic.import.alias.duplicate", "duplicate import alias", decl.Span)
					continue
				}
			} else {
				for name := range a.dependencies[decl.Import.Path] {
					if !isExported(name) {
						continue
					}
					if id, exists := scope.Objects[name]; exists && a.info.Objects[id].Node != decl.NodeID {
						a.addDiagnostic("semantic.import.dot.duplicate", "dot imports declare the same identifier", decl.Span)
					}
				}
			}
			if !used[decl.NodeID] {
				a.addDiagnostic("semantic.import.unused", "imported package is not used", decl.Span)
			}
		}
	}
}
