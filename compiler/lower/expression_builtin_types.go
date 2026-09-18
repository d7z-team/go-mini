package lower

import (
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	check "github.com/d7z-team/mini-go/compiler/semantic"
	"github.com/d7z-team/mini-go/compiler/source"
)

func (l *lowerer) isBuiltinCallName(name string, span source.Span, scope *funcScope) bool {
	if !check.IsPredeclaredBuiltin(strings.TrimSpace(name)) {
		return false
	}
	return !l.isNameBound(name, span, scope)
}

func (l *lowerer) typeConversionCallTarget(expr ast.Expression, scope *funcScope) (string, bool) {
	if expr.Callee == nil || expr.Callee.Kind != ast.ExprIdent {
		return "", false
	}
	if expr.Callee.Name == "type" && expr.Callee.Type.Kind != ast.TypeInvalid {
		target := l.resolveSourceType(expr.Callee.Type)
		return target, target != ""
	}
	name := strings.TrimSpace(expr.Callee.Name)
	if name == "" || l.isValueNameBound(name, expr.Callee.Span, scope) {
		return "", false
	}
	if typ, ok := builtinTypeName(name); ok {
		return typ, true
	}
	if _, ok := l.typeDecls[name]; ok {
		return l.resolveType(name), true
	}
	if _, ok := l.typeAliases[name]; ok {
		return l.resolveType(name), true
	}
	if export, ok := l.dotImportExport(name, expr.Callee.Span); ok && export.Kind == check.ObjectType {
		return l.importedTypeCanonicalName(name, export), true
	}
	return "", false
}

func builtinTypeName(name string) (string, bool) {
	switch strings.TrimSpace(name) {
	case "bool":
		return "Bool", true
	case "string":
		return "String", true
	case "int":
		return "Int", true
	case "int8":
		return "Int8", true
	case "int16":
		return "Int16", true
	case "int32", "rune":
		return "Int32", true
	case "int64":
		return "Int64", true
	case "uint":
		return "Uint", true
	case "uint8", "byte":
		return "Uint8", true
	case "uint16":
		return "Uint16", true
	case "uint32":
		return "Uint32", true
	case "uint64":
		return "Uint64", true
	case "uintptr":
		return "Uintptr", true
	case "float32":
		return "Float32", true
	case "float64":
		return "Float64", true
	case "complex64":
		return "Complex64", true
	case "complex128":
		return "Complex128", true
	default:
		return "", false
	}
}
