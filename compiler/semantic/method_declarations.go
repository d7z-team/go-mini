package semantic

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (a *analyzer) validateMethodDeclarations(program ast.Program) {
	type methodKey struct {
		receiver types.TypeRef
		name     string
	}
	seen := make(map[methodKey]bool)
	for _, file := range program.Files {
		for _, decl := range file.Decls {
			if decl.Kind != ast.DeclFunc || decl.Func.Receiver == nil {
				continue
			}
			receiver := decl.Func.Receiver
			ref := a.info.Relations.ResolveAlias(a.resolvedType(receiver.Type))
			if ref.Kind == types.Pointer {
				ref, _ = a.info.Relations.View(ref).Elem()
			}
			if node, ok := a.info.TypeTable.Node(ref); ok && node.Kind == types.Instance {
				ref = node.Base
			}
			ref = a.info.Relations.ResolveAlias(ref)
			node, ok := a.info.TypeTable.Node(ref)
			shape := a.info.Relations.View(ref).Shape()
			if !ok || node.Kind != types.Named || node.Identity.ModulePath != a.info.ModulePath || shape == types.Interface || shape == types.Pointer {
				a.addDiagnostic("semantic.receiver.type", "method receiver must be a local non-interface type or pointer to one", receiver.Span)
				continue
			}
			if decl.Func.Name == "_" {
				continue
			}
			key := methodKey{receiver: ref, name: decl.Func.Name}
			if seen[key] {
				a.addDiagnostic("semantic.method.duplicate", "duplicate method declaration for receiver type", decl.Span)
			}
			seen[key] = true
		}
	}
}
