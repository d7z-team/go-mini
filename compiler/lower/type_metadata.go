package lower

import (
	"fmt"
	"strings"

	"github.com/d7z-team/mini-go/compiler/ast"
	"github.com/d7z-team/mini-go/compiler/types"
)

type runtimeTypeMethod struct {
	Name       string
	Receiver   string
	Signature  string
	Variadic   bool
	FunctionID string
	ModulePath string
}

func (l *lowerer) bindRuntimeTypeMethods(program ast.Program) error {
	for _, file := range program.Files {
		for _, declaration := range file.Decls {
			if declaration.Kind != ast.DeclType || declaration.Type.Alias {
				continue
			}
			name := strings.TrimSpace(declaration.Type.Name)
			if name == "" || isBlankIdentifier(name) {
				continue
			}
			key := types.TypeKey{ModulePath: l.modulePath, DeclID: types.DeclID(name)}
			node, ok := l.typeTable.Named(key)
			if !ok {
				return fmt.Errorf("type %s has no semantic type node", name)
			}
			declaredMethods := append([]types.Method(nil), node.Methods...)
			methods := l.typeMethods(name, declaration.Type.Type)
			node.Methods = make([]types.Method, 0, len(methods))
			for _, method := range methods {
				receiver, ok := l.typeRef(method.Receiver)
				if !ok {
					return fmt.Errorf("method %s.%s has invalid receiver %q", name, method.Name, method.Receiver)
				}
				for _, declared := range declaredMethods {
					if declared.Name == method.Name && declared.Receiver.Valid() {
						receiver = declared.Receiver
						break
					}
				}
				signatureRef, ok := l.typeRef(method.Signature)
				if !ok {
					return fmt.Errorf("method %s.%s has invalid signature %q", name, method.Name, method.Signature)
				}
				signature, ok := l.typeTable.IsFunction(signatureRef)
				if !ok {
					return fmt.Errorf("method %s.%s signature is not a function", name, method.Name)
				}
				signature.Variadic = signature.Variadic || method.Variadic
				node.Methods = append(node.Methods, types.Method{
					Name: method.Name, Receiver: receiver, Signature: signature,
					FunctionID: method.FunctionID, ModulePath: method.ModulePath,
				})
			}
			if err := l.typeTable.Replace(node); err != nil {
				return fmt.Errorf("type %s methods: %w", name, err)
			}
			underlying := l.typeTable.Underlying(types.TypeRef{Kind: types.Named, Named: key, Node: node.ID})
			if shape, ok := l.typeTable.Node(underlying); ok && shape.Kind == types.Interface && len(shape.Methods) > len(declaration.Type.Type.Methods) {
				shape.Methods = append([]types.Method(nil), shape.Methods[:len(declaration.Type.Type.Methods)]...)
				if err := l.typeTable.Replace(shape); err != nil {
					return fmt.Errorf("type %s interface shape: %w", name, err)
				}
			}
		}
	}
	return nil
}

func (l *lowerer) typeMethods(typeName string, typ ast.TypeExpr) []runtimeTypeMethod {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return nil
	}
	var out []runtimeTypeMethod
	if typ.Kind == ast.TypeInterface {
		methods := l.interfaceTypeMethodMetadata(typeName, typ, map[string]struct{}{})
		for _, method := range methods {
			method.Receiver = typeName
			out = append(out, method)
		}
		sortTypeMethods(out)
		return out
	}
	for key, method := range l.methods {
		receiver := l.typeRefString(method.Receiver)
		if receiver != typeName && receiver != "Ptr<"+typeName+">" {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(key, receiver+"."))
		if name == "" || name == key {
			continue
		}
		out = append(out, runtimeTypeMethod{
			Name:       name,
			Receiver:   receiver,
			Signature:  signatureFromTypes(l.signatureParamTypes(method.Signature), l.signatureResultTypes(method.Signature)),
			Variadic:   method.Signature.Variadic,
			FunctionID: method.FunctionID,
			ModulePath: method.ModulePath,
		})
	}
	existing := map[string]struct{}{}
	for _, method := range out {
		existing[method.Name] = struct{}{}
	}
	for _, method := range l.promotedTypeMethods(typeName, typ, map[string]struct{}{}) {
		if _, ok := existing[method.Name]; ok {
			continue
		}
		out = append(out, method)
		existing[method.Name] = struct{}{}
	}
	sortTypeMethods(out)
	return out
}

func (l *lowerer) interfaceTypeMethodMetadata(typeName string, typ ast.TypeExpr, seen map[string]struct{}) []runtimeTypeMethod {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" || typ.Kind != ast.TypeInterface {
		return nil
	}
	if _, recursive := seen[typeName]; recursive {
		return nil
	}
	seen[typeName] = struct{}{}
	defer delete(seen, typeName)
	methods := map[string]runtimeTypeMethod{}
	for _, embed := range typ.Embeds {
		embedType := l.resolveSourceType(embed)
		if decl, ok := l.typeDeclFor(embedType); ok && decl.Kind == ast.TypeInterface {
			for _, method := range l.interfaceTypeMethodMetadata(embedType, decl, seen) {
				owner := strings.TrimSpace(method.ModulePath)
				if owner == "" {
					owner = l.currentModulePath()
				}
				methods[methodIdentity(owner, method.Name)] = method
			}
			continue
		}
		if export, ok := l.importedTypeInfo(embedType); ok {
			for _, method := range export.Methods {
				name := strings.TrimSpace(method.Name)
				signature := strings.TrimSpace(method.Signature)
				if name == "" || signature == "" {
					continue
				}
				owner := strings.TrimSpace(method.ModulePath)
				if owner == "" {
					owner = strings.TrimSpace(export.ModulePath)
				}
				methods[methodIdentity(owner, name)] = runtimeTypeMethod{
					Name:       name,
					Receiver:   typeName,
					Signature:  signature,
					Variadic:   method.Variadic,
					ModulePath: owner,
				}
			}
			continue
		}
		for identity, signature := range l.interfaceMethods(embedType, seen) {
			name := methodNameFromIdentity(identity)
			methods[identity] = runtimeTypeMethod{Name: name, Receiver: typeName, Signature: signature, ModulePath: methodOwnerFromIdentity(identity)}
		}
	}
	for _, method := range typ.Methods {
		name := strings.TrimSpace(method.Name)
		if name == "" || isBlankIdentifier(name) {
			continue
		}
		owner := ""
		if !isExported(name) {
			owner = l.currentModulePath()
		}
		methods[methodIdentity(owner, name)] = runtimeTypeMethod{
			Name:       name,
			Receiver:   typeName,
			Signature:  l.signatureOf(method),
			Variadic:   funcDeclVariadic(method),
			ModulePath: owner,
		}
	}
	out := make([]runtimeTypeMethod, 0, len(methods))
	for _, method := range methods {
		out = append(out, method)
	}
	sortTypeMethods(out)
	return out
}

func funcDeclVariadic(fn ast.FuncDecl) bool {
	if len(fn.Params) == 0 {
		return false
	}
	return fn.Params[len(fn.Params)-1].Variadic
}

func (l *lowerer) promotedTypeMethods(typeName string, typ ast.TypeExpr, seen map[string]struct{}) []runtimeTypeMethod {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" || typ.Kind != ast.TypeStruct {
		return nil
	}
	if _, recursive := seen[typeName]; recursive {
		return nil
	}
	seen[typeName] = struct{}{}
	defer delete(seen, typeName)
	var out []runtimeTypeMethod
	for _, field := range typ.Fields {
		if strings.TrimSpace(field.Name) != "" {
			continue
		}
		fieldType := l.resolveSourceType(field.Type)
		out = append(out, l.importedPromotedTypeMethods(fieldType)...)
		for _, receiver := range l.promotedReceiverTypes(fieldType) {
			for key, method := range l.methods {
				if l.typeRefString(method.Receiver) != receiver {
					continue
				}
				name := strings.TrimSpace(strings.TrimPrefix(key, receiver+"."))
				if name == "" || name == key {
					continue
				}
				out = append(out, runtimeTypeMethod{
					Name:       name,
					Receiver:   receiver,
					Signature:  signatureFromTypes(l.signatureParamTypes(method.Signature), l.signatureResultTypes(method.Signature)),
					Variadic:   method.Signature.Variadic,
					FunctionID: method.FunctionID,
					ModulePath: method.ModulePath,
				})
			}
		}
		base := fieldType
		if element, ok := l.pointerElementType(fieldType); ok {
			base = element
		}
		if decl, ok := l.typeDecls[base]; ok && decl.Kind == ast.TypeStruct {
			out = append(out, l.promotedTypeMethods(base, decl, seen)...)
		}
	}
	sortTypeMethods(out)
	return out
}

func (l *lowerer) promotedReceiverTypes(fieldType string) []string {
	fieldType = strings.TrimSpace(fieldType)
	if fieldType == "" {
		return nil
	}
	base, ok := l.pointerElementType(fieldType)
	if !ok {
		base = fieldType
	}
	return []string{base, "Ptr<" + base + ">"}
}

func (l *lowerer) importedPromotedTypeMethods(fieldType string) []runtimeTypeMethod {
	out := []runtimeTypeMethod{}
	for _, receiver := range l.promotedReceiverTypes(fieldType) {
		export, ok := l.importedTypeInfo(receiver)
		if !ok {
			continue
		}
		for _, method := range export.Methods {
			name := strings.TrimSpace(method.Name)
			signature := strings.TrimSpace(method.Signature)
			if name == "" || signature == "" {
				continue
			}
			modulePath := strings.TrimSpace(method.ModulePath)
			if modulePath == "" {
				modulePath = strings.TrimSpace(export.ModulePath)
			}
			l.ensureSourceRequirement(modulePath)
			out = append(out, runtimeTypeMethod{
				Name:       name,
				Receiver:   strings.TrimSpace(method.Receiver),
				Signature:  signature,
				Variadic:   method.Variadic,
				FunctionID: strings.TrimSpace(method.FunctionID),
				ModulePath: modulePath,
			})
		}
	}
	sortTypeMethods(out)
	return out
}
