package ffigen

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/types"
	"strings"
)

func flattenInterfaceType(name string, iface *ast.InterfaceType, interfaces map[string]*ast.InterfaceType) (*ast.InterfaceType, error) {
	seenInterfaces := make(map[string]bool)
	seenMethods := make(map[string]string)
	flat := &ast.InterfaceType{Methods: &ast.FieldList{}}

	var visit func(label string, current *ast.InterfaceType) error
	visit = func(label string, current *ast.InterfaceType) error {
		if current == nil {
			return nil
		}
		if seenInterfaces[label] {
			return nil
		}
		seenInterfaces[label] = true
		if current.Methods == nil {
			return nil
		}
		for _, field := range current.Methods.List {
			if len(field.Names) == 0 {
				embeddedName := typeToString(field.Type)
				if local, ok := embeddedInterfaceName(field.Type); ok {
					target, ok := interfaces[local]
					if !ok {
						return fmt.Errorf("embedded interface %s not found", local)
					}
					if err := visit(local, target); err != nil {
						return err
					}
					continue
				}
				methods, err := synthesizeEmbeddedInterfaceMethods(field.Type)
				if err != nil {
					return fmt.Errorf("embedded interface %s: %w", embeddedName, err)
				}
				for _, method := range methods {
					if err := appendFlattenedMethod(flat, seenMethods, method); err != nil {
						return err
					}
				}
				continue
			}
			if err := appendFlattenedMethod(flat, seenMethods, field); err != nil {
				return err
			}
		}
		return nil
	}

	if err := visit(name, iface); err != nil {
		return nil, err
	}
	return flat, nil
}

func embeddedInterfaceName(expr ast.Expr) (string, bool) {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name, true
	default:
		return "", false
	}
}

func appendFlattenedMethod(dst *ast.InterfaceType, seen map[string]string, field *ast.Field) error {
	if field == nil || len(field.Names) == 0 {
		return nil
	}
	methodName := field.Names[0].Name
	funcType, ok := field.Type.(*ast.FuncType)
	if !ok {
		return fmt.Errorf("method %s is not a function", methodName)
	}
	sig := funcTypeKey(funcType)
	if existing, ok := seen[methodName]; ok {
		if existing != sig {
			return fmt.Errorf("method conflict for %s: %s vs %s", methodName, existing, sig)
		}
		return nil
	}
	seen[methodName] = sig
	dst.Methods.List = append(dst.Methods.List, &ast.Field{
		Names: []*ast.Ident{ast.NewIdent(methodName)},
		Type:  cloneFuncType(funcType),
		Doc:   field.Doc,
	})
	return nil
}

func funcTypeKey(fn *ast.FuncType) string {
	if fn == nil {
		return "func()"
	}
	var params []string
	if fn.Params != nil {
		for _, field := range fn.Params.List {
			typeName := typeToString(field.Type)
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				params = append(params, typeName)
			}
		}
	}
	var results []string
	if fn.Results != nil {
		for _, field := range fn.Results.List {
			typeName := typeToString(field.Type)
			count := len(field.Names)
			if count == 0 {
				count = 1
			}
			for i := 0; i < count; i++ {
				results = append(results, typeName)
			}
		}
	}
	return "func(" + strings.Join(params, ",") + ")->(" + strings.Join(results, ",") + ")"
}

func cloneFuncType(fn *ast.FuncType) *ast.FuncType {
	if fn == nil {
		return nil
	}
	cloned := *fn
	if fn.Params != nil {
		cloned.Params = cloneFieldList(fn.Params)
	}
	if fn.Results != nil {
		cloned.Results = cloneFieldList(fn.Results)
	}
	return &cloned
}

func cloneFieldList(list *ast.FieldList) *ast.FieldList {
	if list == nil {
		return nil
	}
	res := &ast.FieldList{Opening: list.Opening, Closing: list.Closing}
	for _, field := range list.List {
		names := make([]*ast.Ident, len(field.Names))
		for i, name := range field.Names {
			if name == nil {
				continue
			}
			names[i] = ast.NewIdent(name.Name)
		}
		res.List = append(res.List, &ast.Field{
			Names:   names,
			Type:    field.Type,
			Doc:     field.Doc,
			Tag:     field.Tag,
			Comment: field.Comment,
		})
	}
	return res
}

func synthesizeEmbeddedInterfaceMethods(expr ast.Expr) ([]*ast.Field, error) {
	typ := typeInfo.TypeOf(expr)
	if typ == nil {
		return nil, errors.New("missing type info")
	}
	if named, ok := typ.(*types.Named); ok {
		typ = named.Underlying()
	}
	iface, ok := typ.Underlying().(*types.Interface)
	if !ok {
		return nil, errors.New("not an interface")
	}
	iface = iface.Complete()
	var methods []*ast.Field
	for i := 0; i < iface.NumMethods(); i++ {
		method := iface.Method(i)
		sig, ok := method.Type().(*types.Signature)
		if !ok {
			return nil, fmt.Errorf("method %s has non-signature type", method.Name())
		}
		funcType, err := parseFuncTypeFromSignature(sig)
		if err != nil {
			return nil, fmt.Errorf("parse method %s signature: %w", method.Name(), err)
		}
		methods = append(methods, &ast.Field{
			Names: []*ast.Ident{ast.NewIdent(method.Name())},
			Type:  funcType,
		})
	}
	return methods, nil
}

func parseFuncTypeFromSignature(sig *types.Signature) (*ast.FuncType, error) {
	qualifier := func(pkg *types.Package) string {
		if pkg == nil {
			return ""
		}
		for alias, path := range knownImports {
			if path == pkg.Path() {
				return alias
			}
		}
		return pkg.Name()
	}
	expr, err := parser.ParseExpr(types.TypeString(sig, qualifier))
	if err != nil {
		return nil, err
	}
	funcType, ok := expr.(*ast.FuncType)
	if !ok {
		return nil, fmt.Errorf("parsed signature is %T", expr)
	}
	return funcType, nil
}
