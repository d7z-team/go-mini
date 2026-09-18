package specialize

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/source"
)

func genericCalleeName(expr ast.Expression) string {
	if expr.Kind == ast.ExprIdent {
		return expr.Name
	}
	if expr.Kind == ast.ExprSelector && expr.Operand != nil && expr.Operand.Kind == ast.ExprIdent {
		return expr.Operand.Name + "." + expr.Field
	}
	return ""
}

func specializationNames(kind, name string, args []ast.TypeExpr) (string, string) {
	parts := make([]string, len(args))
	for i := range args {
		parts[i] = genericTypeText(args[i])
	}
	key := kind + ":" + name + "[" + strings.Join(parts, ",") + "]"
	sum := sha256.Sum256([]byte(key))
	generated := "generic_" + sanitizeGenericName(name) + "_" + hex.EncodeToString(sum[:6])
	return key, generated
}

func genericTypeText(typ ast.TypeExpr) string {
	switch typ.Kind {
	case ast.TypeName:
		return canonicalGenericTypeName(typ.Name)
	case ast.TypePointer:
		if typ.Elem != nil {
			return "Ptr<" + genericTypeText(*typ.Elem) + ">"
		}
	case ast.TypeSlice:
		if typ.Elem != nil {
			return "Slice<" + genericTypeText(*typ.Elem) + ">"
		}
	case ast.TypeMap:
		if typ.Key != nil && typ.Elem != nil {
			return "Map<" + genericTypeText(*typ.Key) + ", " + genericTypeText(*typ.Elem) + ">"
		}
	case ast.TypeArray:
		if typ.Elem != nil {
			length := genericArrayLength(typ)
			if typ.LenInfer {
				length = "..."
			}
			return "Array<" + length + ", " + genericTypeText(*typ.Elem) + ">"
		}
	case ast.TypeChan:
		if typ.Elem != nil {
			prefix := "Waitable<"
			switch typ.Direction {
			case "recv":
				prefix = "ReceiveWaitable<"
			case "send":
				prefix = "SendWaitable<"
			}
			return prefix + genericTypeText(*typ.Elem) + ">"
		}
	case ast.TypeFunc:
		params := make([]string, len(typ.Params))
		for i := range typ.Params {
			params[i] = genericTypeText(typ.Params[i].Type)
			if typ.Params[i].Variadic {
				params[i] = "variadic " + params[i]
			}
		}
		results := make([]string, len(typ.Results))
		for i := range typ.Results {
			results[i] = genericTypeText(typ.Results[i].Type)
		}
		return "function(" + strings.Join(params, ", ") + ") (" + strings.Join(results, ", ") + ")"
	case ast.TypeStruct:
		fields := make([]string, len(typ.Fields))
		for i := range typ.Fields {
			fields[i] = typ.Fields[i].Name + ":" + genericTypeText(typ.Fields[i].Type) + ":" + typ.Fields[i].Tag
		}
		return "struct{" + strings.Join(fields, ",") + "}"
	case ast.TypeInterface:
		members := make([]string, 0, len(typ.Embeds)+len(typ.Terms)+len(typ.Methods))
		for i := range typ.Embeds {
			members = append(members, genericTypeText(typ.Embeds[i]))
		}
		for i := range typ.Terms {
			term := genericTypeText(typ.Terms[i].Type)
			if typ.Terms[i].Approx {
				term = "~" + term
			}
			members = append(members, term)
		}
		for i := range typ.Methods {
			members = append(members, typ.Methods[i].Name+":"+genericTypeText(ast.TypeExpr{Kind: ast.TypeFunc, Params: typ.Methods[i].Params, Results: typ.Methods[i].Results}))
		}
		return "interface{" + strings.Join(members, ",") + "}"
	case ast.TypeInstance:
		if typ.Base != nil {
			parts := make([]string, len(typ.TypeArgs))
			for i := range typ.TypeArgs {
				parts[i] = genericTypeText(typ.TypeArgs[i])
			}
			return genericTypeText(*typ.Base) + "[" + strings.Join(parts, ",") + "]"
		}
	}
	if typ.Name != "" {
		return typ.Name
	}
	return string(typ.Kind)
}

func cloneGenericDecl(decl ast.Decl) ast.Decl {
	return ast.CloneDecl(decl)
}

func sanitizeGenericName(name string) string {
	var builder strings.Builder
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' {
			builder.WriteRune(ch)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func canonicalGenericTypeName(name string) string {
	switch name {
	case "bool":
		return "Bool"
	case "string":
		return "String"
	case "int":
		return "Int"
	case "int8":
		return "Int8"
	case "int16":
		return "Int16"
	case "int32", "rune":
		return "Int32"
	case "int64":
		return "Int64"
	case "uint":
		return "Uint"
	case "uint8", "byte":
		return "Uint8"
	case "uint16":
		return "Uint16"
	case "uint32":
		return "Uint32"
	case "uint64":
		return "Uint64"
	case "uintptr":
		return "Uintptr"
	case "float32":
		return "Float32"
	case "float64":
		return "Float64"
	case "complex64":
		return "Complex64"
	case "complex128":
		return "Complex128"
	case "any":
		return "Any"
	default:
		return name
	}
}

func (s *genericSpecializer) addDiagnostic(code, message string, span source.Span) {
	s.diagnostics.Add(source.Diagnostic{Code: source.DiagnosticCode(code), Severity: source.SeverityError, Message: message, Primary: span})
}
