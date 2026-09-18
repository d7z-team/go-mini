package semantic

import (
	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/source"
	"github.com/d7z-team/mini-go/compiler/types"
)

func (a *analyzer) analyzeEmbedInitializer(decl *ast.ValueDecl, expr *ast.Expression, local bool, span source.Span) {
	if local {
		a.addDiagnostic("semantic.embed.scope", "//go:embed is only permitted on package variables", span)
		return
	}
	if len(decl.Names) != 1 || decl.Names[0] == "_" || len(expr.EmbedFiles) == 0 {
		a.addDiagnostic("semantic.embed.declaration", "//go:embed requires one package variable and at least one matched file", span)
		return
	}
	declared := a.resolvedType(decl.Type)
	if !declared.Valid() {
		a.addDiagnostic("semantic.embed.type", "//go:embed variable must have an explicit string, byte slice, or embed.FS type", span)
		return
	}
	view := a.info.Relations.View(declared)
	kind := EmbedInvalid
	storage := types.TypeRef{}
	if primitive, ok := view.Primitive(); ok && primitive == types.PrimitiveString {
		kind = EmbedString
	} else if view.Shape() == types.Slice {
		if elem, ok := view.Elem(); ok {
			if primitive, ok := a.info.Relations.View(elem).Primitive(); ok && primitive == types.PrimitiveUint8 {
				kind = EmbedBytes
			}
		}
	} else {
		identity := a.resolveAlias(declared)
		if identity.Kind == types.Named && identity.Named.ModulePath == "embed" && identity.Named.DeclID == "FS" {
			kind = EmbedFS
			if fields, ok := a.info.Relations.View(identity).StructFields(); ok {
				for _, field := range fields {
					if field.Name == "files" {
						storage = field.Type
						break
					}
				}
			}
		}
	}
	if kind == EmbedInvalid || kind == EmbedFS && !storage.Valid() {
		a.addDiagnostic("semantic.embed.type", "//go:embed variable must have type string, a byte slice type, or embed.FS (or an alias of embed.FS)", span)
		return
	}
	if kind != EmbedFS && len(expr.EmbedFiles) != 1 {
		a.addDiagnostic("semantic.embed.files", "//go:embed string and byte slice variables must match exactly one file", span)
		return
	}
	a.info.Exprs[expr.NodeID] = ExprInfo{Type: declared, Mode: ExprValue}
	a.info.Embeds[expr.NodeID] = EmbedInfo{Kind: kind, Type: a.resolveAlias(declared), Storage: storage}
}
