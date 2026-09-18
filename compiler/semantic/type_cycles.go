package semantic

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (a *analyzer) validateTypeCycles(program ast.Program) bool {
	valid := true
	for _, file := range program.Files {
		for _, decl := range file.Decls {
			if decl.Kind == ast.DeclType {
				if object, ok := a.info.Lookup(a.info.PackageScope, decl.Type.Name); ok {
					valid = a.validateTypeCycle(object, decl.Span) && valid
				}
			}
		}
	}
	return valid
}

func (a *analyzer) validateTypeCycle(object Object, span source.Span) bool {
	// Alias expansion traverses every component. Defined-type size recursion
	// stops at indirection; interface embedding is a separate direct cycle.
	active := make(map[types.TypeID]bool)
	complete := make(map[types.TypeID]bool)
	var path []string
	var visit func(types.TypeRef) bool
	visit = func(ref types.TypeRef) bool {
		node, ok := a.info.TypeTable.Node(ref)
		if !ok || complete[node.ID] {
			return false
		}
		if active[node.ID] {
			return true
		}
		if node.Kind == types.Named {
			if object.Alias && !node.Alias {
				return false
			}
			path = append(path, string(node.Identity.DeclID))
		}
		active[node.ID] = true
		var refs []types.TypeRef
		switch node.Kind {
		case types.Named:
			if node.Alias {
				refs = append(refs, node.AliasTarget)
			} else {
				refs = append(refs, node.Underlying)
			}
		case types.Struct:
			for _, field := range node.Fields {
				refs = append(refs, field.Type)
			}
		case types.Array:
			refs = append(refs, node.Elem)
		case types.Interface:
			for _, term := range node.Terms {
				refs = append(refs, term.Type)
			}
			if object.Alias {
				for _, method := range node.Methods {
					for _, param := range method.Signature.Params {
						refs = append(refs, param.Type)
					}
					refs = append(refs, method.Signature.Results...)
				}
			}
		case types.Pointer, types.Slice, types.Map, types.Waitable:
			if object.Alias {
				refs = append(refs, node.Key, node.Elem)
			}
		case types.Function:
			if object.Alias && node.Signature != nil {
				for _, param := range node.Signature.Params {
					refs = append(refs, param.Type)
				}
				refs = append(refs, node.Signature.Results...)
			}
		}
		for _, next := range refs {
			if visit(next) {
				return true
			}
		}
		delete(active, node.ID)
		complete[node.ID] = true
		if node.Kind == types.Named {
			path = path[:len(path)-1]
		}
		return false
	}
	if !visit(object.Type) {
		return true
	}
	code, message := "semantic.type.defined.cycle", "invalid recursive type: "
	if object.Alias {
		code, message = "semantic.type.alias.cycle", "type alias cycle: "
	} else if a.info.Relations.View(object.Type).Shape() == types.Interface {
		code, message = "semantic.interface.embed.cycle", "interface embedding cycle: "
	}
	a.addDiagnostic(code, message+strings.Join(append(path, object.Name), " -> "), span)
	return false
}
